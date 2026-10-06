package sup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryLifecycleHookAndClear(t *testing.T) {
	root := t.TempDir()
	history := filepath.Join(root, "data", "docs", "history")
	mount := filepath.Join(root, "mount")
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	// Substitute only the native mount/exec commands; execute the actual hook.
	sbx := `#!/bin/sh
set -eu
case "$1" in
  mount)
    source=${3%%:*}
    target=${3#*:}; target=${target%:rw}
    test -L "$target" || ln -s "$source" "$target"
    ;;
  exec) shift 3; exec "$@" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "sbx"), []byte(sbx), 0700); err != nil {
		t.Fatal(err)
	}
	runHook := func(home string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(home, ".codex"), 0700); err != nil {
			t.Fatal(err)
		}
		config := filepath.Join(home, ".codex", "config.toml")
		if _, err := os.Stat(config); os.IsNotExist(err) {
			if err := os.WriteFile(config, []byte("model = \"chosen\"\n[projects.\"/workspace\"]\ntrust_level = \"trusted\"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte("private"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		command := strings.ReplaceAll(historyCommand(history), historyMount, mount)
		cmd := exec.Command("sh", "-eu", "-c", command)
		cmd.Env = append(os.Environ(), "HOME="+home, "SBX_SANDBOX_NAME=docs", "PATH="+bin+":"+os.Getenv("PATH"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	first := filepath.Join(root, "first-home")
	runHook(first)
	session := filepath.Join(first, ".codex", "sessions", "conversation.jsonl")
	if err := os.WriteFile(session, []byte("conversation"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(session, filepath.Join(first, ".codex", "archived_sessions", "conversation.jsonl")); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(first, ".claude", "projects", "conversation.jsonl")
	if err := os.WriteFile(claude, []byte("claude conversation"), 0600); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(root, "second-home")
	runHook(second)
	runHook(second)
	for _, relative := range []string{".codex/archived_sessions/conversation.jsonl", ".claude/projects/conversation.jsonl"} {
		if _, err := os.ReadFile(filepath.Join(second, relative)); err != nil {
			t.Fatal("conversation did not survive recreation", err)
		}
	}
	config, _ := os.ReadFile(filepath.Join(second, ".codex", "config.toml"))
	if strings.Count(string(config), "sqlite_home =") != 1 || !strings.Contains(string(config), `model = "chosen"`) {
		t.Fatal("configuration was lost or duplicated")
	}
	if _, err := os.Stat(filepath.Join(history, "codex", "auth.json")); !os.IsNotExist(err) {
		t.Fatal("credential persisted", err)
	}
	r := projectRuntime{dataRoot: filepath.Join(root, "data")}
	if err := r.clearHistory(&projectRecord{Name: "docs", History: history}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(second, ".codex", "sessions")); err != nil {
		t.Fatal("clear broke a directory link", err)
	}
	if _, err := os.Stat(filepath.Join(second, ".claude", "history.jsonl")); err != nil {
		t.Fatal("clear broke a file link", err)
	}
	if _, err := os.Stat(claude); !os.IsNotExist(err) {
		t.Fatal("clear retained a transcript", err)
	}
}

func TestHistoryHookRefusesLocalConversations(t *testing.T) {
	home := t.TempDir()
	sessions := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(sessions, 0700); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(sessions, "keep.jsonl")
	if err := os.WriteFile(transcript, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	// Extract the guest block verbatim from the native lifecycle hook.
	_, script, _ := strings.Cut(historyHook, "<<'GUEST_SETUP'\n")
	script, _, _ = strings.Cut(script, "\nGUEST_SETUP\n")
	cmd := exec.Command("sh", "-eu", "-c", script, "sh", filepath.Join(home, "history"))
	cmd.Env = append(os.Environ(), "HOME="+home)
	if _, err := cmd.CombinedOutput(); err == nil {
		t.Fatal("expected refusal to hide local history")
	}
	if data, err := os.ReadFile(transcript); err != nil || string(data) != "keep" {
		t.Fatal("local history lost", err)
	}
}
