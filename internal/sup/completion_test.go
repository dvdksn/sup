package sup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCompletionNames(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("PATH", "") // No sbx, or any other subprocess, is available.
	check := func(prefix, want string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code, err := Run([]string{"__complete", prefix}, nil, &out, &stderr)
		if code != 0 || err != nil || out.String() != want || stderr.Len() != 0 {
			t.Fatalf("completion: %d %v %q %q", code, err, out.String(), stderr.String())
		}
	}
	check("", "")
	stateRoot := filepath.Join(root, "sup")
	if _, err := os.Stat(stateRoot); !os.IsNotExist(err) {
		t.Fatal("completion created state")
	}
	saveFixture(t, stateRoot, "docker-docs")
	saveFixture(t, stateRoot, "docker-docs-pr-123")
	saveFixture(t, stateRoot, "another-env")
	saveFixture(t, stateRoot, "broken-env")
	if err := os.WriteFile(filepath.Join(stateRoot, "broken-env", "state.json"), []byte("bad json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stateRoot, ".locks"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "config", "sup"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config", "sup", "config.lua"), []byte(`error("must not load")`), 0600); err != nil {
		t.Fatal(err)
	}
	check("docker-", "docker-docs\ndocker-docs-pr-123\n")
	check("", "another-env\ndocker-docs\ndocker-docs-pr-123\n")
	check("missing", "")
}

func TestCompletionScript(t *testing.T) {
	t.Setenv("HOME", "")
	var out, stderr bytes.Buffer
	code, err := Run([]string{"completion", "bash"}, nil, &out, &stderr)
	if code != 0 || err != nil || out.String() != bashCompletion {
		t.Fatal(code, err)
	}
	for _, args := range [][]string{{"completion"}, {"completion", "fish"}, {"completion", "bash", "extra"}, {"__complete"}} {
		if _, err := Run(args, nil, &out, &stderr); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
