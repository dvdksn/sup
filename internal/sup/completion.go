package sup

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//go:embed completion.bash
var bashCompletion string

// Completion reads saved project names without contacting SBX.
func completeNames(args []string, out io.Writer) (int, error) {
	if len(args) != 1 {
		return 1, errors.New("expected a completion prefix")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 1, err
	}
	root := filepath.Join(xdg("XDG_STATE_HOME", filepath.Join(home, ".local", "state")), "sup")
	if !filepath.IsAbs(root) {
		return 1, errors.New("XDG_STATE_HOME must be an absolute path")
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 1, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !namePattern.MatchString(name) || !strings.HasPrefix(name, args[0]) {
			continue
		}
		if _, err := (projectRuntime{root: root}).load(name); err == nil {
			if _, err = fmt.Fprintln(out, name); err != nil {
				return 1, err
			}
			continue
		}

	}
	return 0, nil
}
