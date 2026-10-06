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
	if !strings.Contains(out.String(), "--env-file") {
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
