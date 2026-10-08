package sup

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIHelp(t *testing.T) {
	var out bytes.Buffer
	if code, err := Run([]string{"--help"}, nil, &out, &out); code != 0 || err != nil {
		t.Fatal(code, err)
	}
	if !strings.Contains(out.String(), "--via") {
		t.Fatal("incorrect CLI help")
	}
}

func TestCLIEntrypoints(t *testing.T) {
	for _, agent := range []string{"", "codex", "claude"} {
		args := []string{"docker/docs"}
		if agent != "" {
			args = append(args, "--agent", agent)
		}
		o, err := parseProjects(args)
		if err != nil || o.agent != agent {
			t.Fatal(args, o.agent, err)
		}
	}
	for _, agent := range []string{"shell", "bash", "unknown"} {
		if _, err := parseProjects([]string{"docker/docs", "--agent", agent}); err == nil {
			t.Fatalf("accepted unsupported agent %s", agent)
		}
	}
}

func TestCommandExitStatus(t *testing.T) {
	var out bytes.Buffer
	r := projectRuntime{out: &out, stderr: &out}
	_, err := r.command("sh", false, "-c", "exit 17")
	if code, _ := projectResult(err); code != 17 {
		t.Fatal("subprocess exit status lost", code, err)
	}
}

func TestCLIRejectsEnvironmentConfiguration(t *testing.T) {
	for _, flag := range []string{"--env-file", "--env-arg", "--kit", "--cwd", "--no-history", "--name", "--auto-approve", "-y"} {
		if _, err := parseProjects([]string{"docker/docs", flag, "value"}); err == nil {
			t.Fatalf("accepted configuration flag %s", flag)
		}
	}
}

func TestCLIRejectsDetachedFrontendBeforeProvisioning(t *testing.T) {
	for _, args := range [][]string{
		{"docker/docs", "--via", "herdr", "-d"},
		{"docker/docs", "--detach", "--via", "herdr"},
		{"docker/docs", "--via", "ssh", "-d"},
		{"docker/docs", "--via", "terminal", "-d"},
		{"open", "docker/docs", "-d", "--via", "herdr"},
		{"recreate", "docker/docs", "--force", "--via", "herdr", "-d"},
	} {
		f := newProjectFixture(t)
		err := f.run(t, args...)
		if err == nil || !strings.Contains(err.Error(), "--detach cannot be combined with --via") {
			t.Fatal("missing actionable conflict error", args, err)
		}
		if len(f.calls) != 0 {
			t.Fatal("conflicting options reached sandbox commands", f.calls)
		}
	}
}

func TestCompletionOffersSavedRepositoriesAndNames(t *testing.T) {
	f := newProjectFixture(t)
	t.Setenv("XDG_STATE_HOME", f.home)
	f.runtime.root = filepath.Join(f.home, "sup")
	f.start(t)
	for prefix, want := range map[string]string{"docker/": "docker/docs\n", "docker-": "docker-docs\n"} {
		var out bytes.Buffer
		code, err := completeNames([]string{prefix}, &out)
		if err != nil || code != 0 || out.String() != want {
			t.Fatal(prefix, out.String(), code, err)
		}
	}
	if err := f.run(t, "rm", "docker/docs", "--force"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	code, err := completeNames([]string{"docker"}, &out)
	if err != nil || code != 0 || out.Len() != 0 {
		t.Fatal("removed project remained in completion", out.String(), code, err)
	}
}
