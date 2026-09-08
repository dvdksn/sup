package sup

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLifecycle(t *testing.T) {
	root := t.TempDir()
	configRoot := filepath.Join(root, "config")
	stateRoot := filepath.Join(root, "state")
	bin := filepath.Join(root, "bin")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("XDG_STATE_HOME", stateRoot)
	if err := os.MkdirAll(filepath.Join(configRoot, "sup"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(configRoot, "sup", "config.lua")
	content, err := os.ReadFile("../../examples/config.lua")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(config, content, 0600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "log")
	t.Setenv("SUP_TEST_LOG", log)
	t.Setenv("SUP_TEST_EXIT", "0")
	if err = os.WriteFile(filepath.Join(bin, "sbx"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SUP_TEST_LOG\"\nexit \"$SUP_TEST_EXIT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	run := func(args ...string) {
		t.Helper()
		code, err := Run(args, strings.NewReader(""), io.Discard, io.Discard)
		if err != nil || code != 0 {
			t.Fatalf("%v: code=%d error=%v", args, code, err)
		}
	}
	stateFile := filepath.Join(stateRoot, "sup", "docker-docs", "state.json")
	run("docker/docs", "--plan")
	if _, err = os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatal("plan persisted state")
	}
	run("docker/docs", "--kit", "browser", "-d")
	data, _ := os.ReadFile(log)
	if !strings.HasPrefix(string(data), "env\ncreate\n") {
		t.Fatalf("wrong invocation: %s", data)
	}
	var state snapshot
	data, err = os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Env.Kits) != 4 {
		t.Fatal("wrong snapshot")
	}
	// Names ending in .lock must not collide with another environment's lock.
	run("docker/docs", "--name", "foo.lock", "-d")
	run("docker/docs", "--name", "foo", "-d")
	run("foo.lock")
	run("docker/docs", "-a", "pr=123", "-d")
	run("docker/docs", "-a", "ref=feature/foo", "-d")
	run("docker/docs", "-a", "pr=456", "--name", "explicit-review", "-d")
	run("docker-docs")
	run("docker/docs", "-a", "pr=123")
	run("docker/docs", "-a", "ref=feature/foo")
	run("docker/docs", "-a", "pr=456", "--name", "explicit-review")
	// Config edits cannot silently change a saved environment.
	if err = os.WriteFile(config, []byte(`error('must not run')`), 0600); err != nil {
		t.Fatal(err)
	}
	run("docker-docs-pr-123")
	for _, selection := range [][]string{
		{"docker/docs", "-a", "pr=457", "--name", "explicit-review"},
		{"docker/docs", "-a", "ref=main", "--name", "explicit-review"},
		{"docker/docs", "-a", "pr=123", "--name", "docker-docs"},
	} {
		if _, err := Run(selection, nil, io.Discard, io.Discard); err == nil {
			t.Fatalf("accepted conflicting selection: %v", selection)
		}
	}
	data, _ = os.ReadFile(log)
	if !strings.HasPrefix(string(data), "env\nrun\n") {
		t.Fatal("did not run")
	}
	for _, args := range [][]string{{"docker/docs", "--kit", "vale"}, {"other/repo", "--name", "docker-docs"}, {"missing"}} {
		if _, err := Run(args, strings.NewReader(""), io.Discard, io.Discard); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	t.Setenv("SUP_TEST_EXIT", "7")
	code, err := Run([]string{"docker-docs"}, strings.NewReader(""), io.Discard, io.Discard)
	if code != 7 || err != nil {
		t.Fatalf("exit code lost: %d %v", code, err)
	}
	if _, err = os.Stat(filepath.Join(stateRoot, "sup", ".locks", "docker-docs")); !os.IsNotExist(err) {
		t.Fatal("lock left behind")
	}
	// Legacy snapshots can reconnect, but cannot assert a selector they never saved.
	state.Version = 1
	legacy, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(stateFile, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUP_TEST_EXIT", "0")
	run("docker-docs")
	if _, err := Run([]string{"docker/docs", "--name", "docker-docs", "-a", "pr=123"}, nil, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "predates") {
		t.Fatalf("legacy selector: %v", err)
	}
	// Corrupted state cannot introduce unvalidated fields such as a workspace.
	if err = os.WriteFile(stateFile, []byte(`{"version":1,"repo":"docker/docs","env":{"workspace":"/tmp"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run([]string{"docker-docs"}, nil, io.Discard, io.Discard); err == nil {
		t.Fatal("accepted corrupt state")
	}
}
func TestArgumentBoundary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sbx")
	log := filepath.Join(root, "log")
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SUP_TEST_LOG", log)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s' \"$3\" > \"$SUP_TEST_LOG\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	literal := "a'b $(false) `false`\nline"
	code, err := runSBX("plan", literal, nil, io.Discard, io.Discard)
	if code != 0 || err != nil {
		t.Fatal(code, err)
	}
	data, _ := os.ReadFile(log)
	if string(data) != literal {
		t.Fatal("argument changed")
	}
}
