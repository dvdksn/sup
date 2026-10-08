package sup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type projectFixture struct {
	runtime                   projectRuntime
	home                      string
	live                      map[string]liveSandbox
	calls                     [][]string
	next                      int
	environment               string
	fetches                   int
	fetchStatus               int
	failCreate, cancelRemoval bool
}

func newProjectFixture(t *testing.T) *projectFixture {
	t.Helper()
	root := t.TempDir()
	f := &projectFixture{home: root, live: map[string]liveSandbox{}}
	f.environment = "schemaVersion: \"1\"\nname: dev\nagent: kit-shell\nargs:\n  repo:\n    required: true\nkits:\n  - source: ghcr.io/dvdksn/kit-shell:latest\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		f.fetches++
		if f.fetchStatus != 0 {
			w.WriteHeader(f.fetchStatus)
		}
		fmt.Fprint(w, f.environment)
	}))
	t.Cleanup(server.Close)
	f.runtime = projectRuntime{environmentURL: server.URL, root: filepath.Join(root, "state"), out: io.Discard, stderr: io.Discard, runner: f.command}
	return f
}

// SBX commands and the upstream HTTP endpoint are substituted; saved files are real.
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
		return json.Marshal(items)
	}
	if args[0] == "stop" {
		item := f.live[args[1]]
		item.Status = "stopped"
		f.live[args[1]] = item
	}
	if args[0] == "env" {
		name := args[3] // Native flags must precede file operands.
		if len(args) < 7 || args[4] != "--env-arg" || !strings.HasPrefix(args[5], "repo=") {
			return nil, fmt.Errorf("missing repository argument: %v", args)
		}
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
		t.Fatal("successful creation did not mark project ready")
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
			t.Fatal("Sup must not mount host directories")
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
		t.Fatal("expected creation failure")
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

func TestRemovalForgetsProject(t *testing.T) {
	f := newProjectFixture(t)
	f.start(t)
	original := f.project(t).SandboxID
	if err := f.run(t, "rm", "docker/docs", "--force"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(f.runtime.path("docker-docs"))); !os.IsNotExist(err) {
		t.Fatal("removal retained project files", err)
	}
	var out bytes.Buffer
	f.runtime.out = &out
	if err := f.run(t, "ls", "--json"); err != nil || out.String() != "[]\n" {
		t.Fatal("removed project remained in the listing", out.String(), err)
	}
	f.start(t)
	if f.project(t).SandboxID == original || f.project(t).Repo != "docker/docs" {
		t.Fatal("repository could not create a fresh sandbox after removal")
	}
}

func TestCancelledRemovalAndUnrelatedSandbox(t *testing.T) {
	f := newProjectFixture(t)
	f.start(t)
	original := f.project(t).SandboxID
	f.cancelRemoval = true
	if err := f.run(t, "recreate", "docker-docs", "--force", "-d"); err == nil {
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

func TestPlanDoesNotSaveProject(t *testing.T) {
	f := newProjectFixture(t)
	if err := f.run(t, "docker/docs", "--plan"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.runtime.path("docker-docs")); !os.IsNotExist(err) {
		t.Fatal("plan saved a project", err)
	}
	if f.fetches != 1 {
		t.Fatal("plan did not fetch upstream environment", f.fetches)
	}
	call := f.calls[len(f.calls)-1]
	if !reflect.DeepEqual(call[:7], []string{"sbx", "env", "plan", "--name", "docker-docs", "--env-arg", "repo=docker/docs"}) {
		t.Fatal("plan did not supply project arguments", call)
	}
}

func TestFetchedEnvironmentAndSavedSnapshot(t *testing.T) {
	f := newProjectFixture(t)
	f.start(t)
	path := f.runtime.envPath("docker-docs")
	original, err := os.ReadFile(path)
	if err != nil || string(original) != f.environment || f.fetches != 1 {
		t.Fatal("creation did not save upstream environment", string(original), f.fetches, err)
	}
	f.environment += "  - source: ghcr.io/dvdksn/kit-rumdl:latest\n"
	f.fetchStatus = http.StatusServiceUnavailable
	for _, args := range [][]string{{"docker-docs", "-d"}, {"stop", "docker-docs"}, {"docker-docs", "-d"}} {
		if err := f.run(t, args...); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(original) || f.fetches != 1 {
		t.Fatal("reopen fetched or replaced the saved environment", f.fetches, err)
	}
	originalID := f.project(t).SandboxID
	if err := f.run(t, "recreate", "docker-docs", "--force", "-d"); err == nil {
		t.Fatal("recreation ignored fetch failure")
	}
	if f.project(t).SandboxID != originalID || f.live["docker-docs"].ID != originalID {
		t.Fatal("fetch failure removed the existing sandbox")
	}
	f.fetchStatus = 0
	if err := f.run(t, "recreate", "docker-docs", "--force", "-d"); err != nil {
		t.Fatal(err)
	}
	after, err = os.ReadFile(path)
	if err != nil || string(after) != f.environment || f.fetches != 3 {
		t.Fatal("recreation did not fetch the updated environment", string(after), f.fetches, err)
	}
}

func TestEnvironmentFetchFailureDoesNotProvision(t *testing.T) {
	f := newProjectFixture(t)
	f.fetchStatus = http.StatusNotFound
	if err := f.run(t, "docker/docs", "-d"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatal("missing fetch error", err)
	}
	if f.next != 0 {
		t.Fatal("provisioned after fetch failure")
	}
	if _, err := os.Stat(f.runtime.path("docker-docs")); !os.IsNotExist(err) {
		t.Fatal("fetch failure saved project", err)
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
				approved = approved || arg == "--force"
			}
			if !approved {
				t.Fatal("creation required plan approval", call)
			}
		}
		if len(call) > 2 && call[1] == "exec" && call[2] == "-it" {
			want := []string{"sbx", "exec", "-it", "--workdir", projectDirectory, "docker-docs", "bash", "-il"}
			if agents == 1 {
				want = append(want[:7], "-lc", "cd "+shellQuote(projectDirectory)+" && exec claude")
			}
			if !reflect.DeepEqual(call, want) {
				t.Fatal("launch did not attach to the selected agent", call)
			}
			agents++
		}
	}
	if creates != 2 || agents != 2 {
		t.Fatal("unexpected creation or attachment count", creates, agents)
	}
}

func TestAgentEntrypoints(t *testing.T) {
	for _, agent := range []string{"codex", "claude"} {
		t.Run(agent, func(t *testing.T) {
			f := newProjectFixture(t)
			if err := f.run(t, "docker/docs", "--agent", agent); err != nil {
				t.Fatal(err)
			}
			want := []string{"sbx", "exec", "-it", "--workdir", projectDirectory, "docker-docs", "bash", "-lc", "cd " + shellQuote(projectDirectory) + " && exec " + agent}
			if got := f.calls[len(f.calls)-1]; !reflect.DeepEqual(got, want) {
				t.Fatal("unexpected agent entrypoint", got)
			}
		})
	}
}

func TestRepositoriesWithAmbiguousNamesStaySeparate(t *testing.T) {
	f := newProjectFixture(t)
	repositories := []string{"a-b/c", "a/b-c", "a/b_c"}
	names := map[string]bool{}
	for _, repo := range repositories {
		if err := f.run(t, repo, "-d"); err != nil {
			t.Fatal(err)
		}
		_, name, err := identity(repo)
		if err != nil || names[name] {
			t.Fatal("repositories share a sandbox name", name, err)
		}
		names[name] = true
		p, err := f.runtime.load(name)
		if err != nil || p.Repo != repo {
			t.Fatal("repository record was conflated", repo, err)
		}
	}
	for _, repo := range repositories {
		if err := f.run(t, strings.ToUpper(repo), "-d"); err != nil {
			t.Fatal(err)
		}
	}
	if f.next != len(repositories) {
		t.Fatal("reopening created another sandbox", f.next)
	}
}
