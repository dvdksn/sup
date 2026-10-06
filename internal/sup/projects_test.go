package sup

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type projectFixture struct {
	runtime                   projectRuntime
	home                      string
	live                      map[string]liveSandbox
	calls                     [][]string
	next                      int
	failCreate, cancelRemoval bool
}

func newProjectFixture(t *testing.T) *projectFixture {
	t.Helper()
	root := t.TempDir()
	f := &projectFixture{home: root, live: map[string]liveSandbox{}}
	f.runtime = projectRuntime{root: filepath.Join(root, "state"), dataRoot: filepath.Join(root, "data"), out: io.Discard, stderr: io.Discard, runner: f.command}
	return f
}

// Only the SBX command boundary is substituted; records and generated files are real.
func (f *projectFixture) command(program string, capture bool, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{program}, args...))
	if program != "sbx" {
		return nil, nil
	}
	if args[0] == "ls" {
		items := []liveSandbox{}
		for _, item := range f.live {
			items = append(items, item)
		}
		return json.Marshal(map[string]any{"sandboxes": items})
	}
	if args[0] == "stop" {
		item := f.live[args[1]]
		item.Status = "stopped"
		f.live[args[1]] = item
	}
	if args[0] == "env" {
		name := args[3] // Native flags must precede file operands.
		switch args[1] {
		case "run":
			f.next++
			f.live[name] = liveSandbox{Name: name, ID: fmt.Sprint(f.next), Status: "running"}
			if f.failCreate {
				return nil, processExit{17}
			}
		case "exec":
			item := f.live[name]
			item.Status = "running"
			f.live[name] = item
		case "rm":
			if !f.cancelRemoval {
				delete(f.live, name)
			}
		}
	}
	return nil, nil
}
func (f *projectFixture) run(t *testing.T, args ...string) error {
	t.Helper()
	options, err := parseProjects(args)
	if err != nil {
		return err
	}
	return f.runtime.run(options)
}
func (f *projectFixture) start(t *testing.T) {
	t.Helper()
	if err := f.run(t, "docker/docs", "-d"); err != nil {
		t.Fatal(err)
	}
}
func (f *projectFixture) project(t *testing.T) *projectRecord {
	t.Helper()
	p, err := f.runtime.load("docker-docs")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProjectReopenAndRecreation(t *testing.T) {
	f := newProjectFixture(t)
	f.start(t)
	original := f.project(t)
	if !original.Ready {
		t.Fatal("successful lifecycle hook did not mark project ready")
	}
	if err := os.MkdirAll(original.History, 0700); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(original.History, "conversation.jsonl")
	if err := os.WriteFile(transcript, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"stop", "docker-docs"}, {"docker-docs", "-d"}} {
		if err := f.run(t, args...); err != nil {
			t.Fatal(err)
		}
	}
	if f.project(t).SandboxID != original.SandboxID {
		t.Fatal("reopening replaced the machine")
	}
	if err := f.run(t, "recreate", "docker-docs", "--force", "-d"); err != nil {
		t.Fatal(err)
	}
	if f.project(t).SandboxID == original.SandboxID {
		t.Fatal("recreation reused the old machine")
	}
	if data, err := os.ReadFile(transcript); err != nil || string(data) != "keep" {
		t.Fatal("history did not survive recreation", err)
	}
	creates, reopens := 0, 0
	for _, call := range f.calls {
		if len(call) > 2 && call[1] == "env" {
			if call[2] == "run" {
				creates++
			}
			if call[2] == "exec" {
				reopens++
			}
		}
		if len(call) > 1 && call[1] == "mount" {
			t.Fatal("mounting must be owned by SBX's lifecycle hook")
		}
	}
	if creates != 2 || reopens != 1 {
		t.Fatal("unexpected provisioning", creates, reopens)
	}
}

func TestFailedCreateCannotOpenAgent(t *testing.T) {
	f := newProjectFixture(t)
	f.failCreate = true
	if err := f.run(t, "docker/docs"); err == nil {
		t.Fatal("expected hook failure")
	}
	if f.project(t).Ready {
		t.Fatal("failed creation was marked ready")
	}
	before := len(f.calls)
	if err := f.run(t, "docker-docs"); err == nil || !strings.Contains(err.Error(), "recreate") {
		t.Fatal("expected explicit repair", err)
	}
	for _, call := range f.calls[before:] {
		if len(call) > 1 && call[1] == "exec" {
			t.Fatal("agent attached after failed creation")
		}
	}
	f.failCreate = false
	if err := f.run(t, "recreate", "docker-docs", "--force", "-d"); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledRemovalAndUnrelatedSandbox(t *testing.T) {
	f := newProjectFixture(t)
	f.start(t)
	original := f.project(t).SandboxID
	f.cancelRemoval = true
	if err := f.run(t, "recreate", "docker-docs", "-d"); err == nil {
		t.Fatal("recreation proceeded after cancellation")
	}
	if f.next != 1 || f.project(t).SandboxID != original {
		t.Fatal("cancelled removal changed the project")
	}
	f.cancelRemoval = false
	f.live["docker-docs"] = liveSandbox{Name: "docker-docs", ID: "unrelated", Status: "running"}
	if err := f.run(t, "rm", "docker-docs", "--force"); err == nil {
		t.Fatal("removed an unrelated sandbox")
	}
}

func TestGeneratedEnvironmentOwnsHistoryHook(t *testing.T) {
	f := newProjectFixture(t)
	f.start(t)
	data, err := os.ReadFile(f.runtime.envPath("docker-docs"))
	if err != nil {
		t.Fatal(err)
	}
	command := string(data)
	if !strings.Contains(command, "postCreate:") || !strings.Contains(command, "command: |") || !strings.Contains(command, `sbx mount "$SBX_SANDBOX_NAME"`) {
		t.Fatal("expected native mounting declared in a readable environment hook")
	}
	f = newProjectFixture(t)
	if err := f.run(t, "docker/docs", "--plan"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.runtime.path("docker-docs")); !os.IsNotExist(err) {
		t.Fatal("plan saved a project", err)
	}
}

func TestEmbeddedEnvironmentAndSavedSnapshot(t *testing.T) {
	f := newProjectFixture(t)
	config := filepath.Join(f.home, "config")
	t.Setenv("XDG_CONFIG_HOME", config)
	if err := os.MkdirAll(filepath.Join(config, "sup"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "sup", "sbxenv.yaml"), []byte("external config must be ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	f.start(t)
	path := f.runtime.envPath("docker-docs")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(original)
	for _, required := range []string{`name: "docker-docs"`, `default: "docker/docs"`, "kit-claude-mixin:", "kit-codex-mixin:", "postCreate:"} {
		if !strings.Contains(content, required) {
			t.Fatal("incomplete embedded environment", required)
		}
	}
	if strings.Contains(content, "[[ quote") || strings.Contains(content, "[[ indent") || strings.Contains(content, "external config") {
		t.Fatal("environment was not rendered solely from the bundle")
	}
	snapshot := append([]byte("# saved environment\n"), original...)
	if err := os.WriteFile(path, snapshot, 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.run(t, "docker-docs", "-d"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(snapshot) {
		t.Fatal("reopen replaced the existing machine's environment")
	}
	if err := f.run(t, "recreate", "docker-docs", "--force", "-d"); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatal("recreation did not use the embedded environment")
	}
}

func TestRepositoryLaunchApprovesAndReusesMachine(t *testing.T) {
	f := newProjectFixture(t)
	if err := f.run(t, "Docker/Docs.git"); err != nil {
		t.Fatal(err)
	}
	original := f.project(t)
	if original.Repo != "docker/docs" {
		t.Fatal("repository identity was not normalized", original.Repo)
	}
	if err := f.run(t, "docker/docs", "--agent", "claude"); err != nil {
		t.Fatal(err)
	}
	if f.next != 1 || f.project(t).SandboxID != original.SandboxID {
		t.Fatal("repository spelling or agent choice created another sandbox")
	}
	if err := f.run(t, "recreate", "docker/docs", "--force", "-d"); err != nil {
		t.Fatal(err)
	}
	creates, agents := 0, 0
	for _, call := range f.calls {
		if len(call) > 2 && call[1] == "env" && call[2] == "run" {
			creates++
			approved := false
			for _, arg := range call {
				approved = approved || arg == "--auto-approve"
			}
			if !approved {
				t.Fatal("creation required plan approval", call)
			}
		}
		if len(call) > 2 && call[1] == "exec" && call[2] == "-it" {
			want := "codex"
			if agents == 1 {
				want = "claude"
			}
			if !strings.HasSuffix(call[len(call)-1], "exec "+want) {
				t.Fatal("launch did not attach to the selected agent", call)
			}
			agents++
		}
	}
	if creates != 2 || agents != 2 {
		t.Fatal("unexpected creation or attachment count", creates, agents)
	}
}
