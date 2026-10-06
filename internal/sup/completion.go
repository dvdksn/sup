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

// Completion reads saved repositories and names without contacting SBX.
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
		if !entry.IsDir() || !namePattern.MatchString(name) {
			continue
		}
		p, err := (projectRuntime{root: root}).load(name)
		if err != nil {
			continue
		}
		for _, candidate := range []string{p.Repo, p.Name} {
			if strings.HasPrefix(candidate, args[0]) {
				if _, err = fmt.Fprintln(out, candidate); err != nil {
					return 1, err
				}
			}
		}
	}
	return 0, nil
}
