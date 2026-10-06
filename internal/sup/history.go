package sup

import (
	_ "embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const historyMount = "/home/agent/project-history"

//go:embed history.sh
var historyHook string

func historyCommand(root string) string {
	return "history_root=" + shellQuote(root) + "\n" + historyHook
}

func (r projectRuntime) clearHistory(p *projectRecord) error {
	if filepath.Clean(p.History) != filepath.Join(r.dataRoot, p.Name, "history") {
		return errors.New("history is outside this project's data directory")
	}
	if info, err := os.Lstat(p.History); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("history root must be a real directory")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// Keep directories: stopped machines retain a mount and symlinks to these paths.
	if err := filepath.WalkDir(p.History, func(path string, entry fs.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		return os.Remove(path)
	}); err != nil {
		return err
	}
	for _, relative := range []string{"codex/history.jsonl", "codex/session_index.jsonl", "claude/history.jsonl"} {
		path := filepath.Join(p.History, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}
