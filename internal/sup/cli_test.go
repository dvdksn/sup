package sup

import (
	"bytes"
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

func TestCommandExitStatus(t *testing.T) {
	var out bytes.Buffer
	r := projectRuntime{out: &out, stderr: &out}
	_, err := r.command("sh", false, "-c", "exit 17")
	if code, _ := projectResult(err); code != 17 {
		t.Fatal("subprocess exit status lost", code, err)
	}
}

func TestCLIRejectsEnvironmentConfiguration(t *testing.T) {
	for _, flag := range []string{"--env-file", "--env-arg", "--kit", "--cwd", "--no-history"} {
		if _, err := parseProjects([]string{"docker/docs", flag, "value"}); err == nil {
			t.Fatalf("accepted configuration flag %s", flag)
		}
	}
}
