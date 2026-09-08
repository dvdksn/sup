package sup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
)

func inventory() (map[string]string, error) {
	cmd := exec.Command("sbx", "ls", "--json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("query sandbox status: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var result struct {
		Sandboxes *[]struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"sandboxes"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode sbx inventory: %w", err)
	}
	if result.Sandboxes == nil {
		return nil, errors.New("sbx inventory is missing the sandboxes list")
	}
	statuses := map[string]string{}
	for _, s := range *result.Sandboxes {
		if s.Name == "" || s.Status == "" {
			return nil, errors.New("sbx inventory contains an unnamed sandbox or missing status")
		}
		statuses[s.Name] = s.Status
	}
	return statuses, nil
}
func list(root string, out, stderr io.Writer) (int, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		fmt.Fprintln(out, "No saved environments.")
		return 0, nil
	}
	if err != nil {
		return 1, err
	}
	var states []snapshot
	code := 0
	for _, entry := range entries {
		if !entry.IsDir() || !namePattern.MatchString(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name(), "state.json"))
		if os.IsNotExist(err) {
			continue
		}
		var state snapshot
		if err == nil {
			state, err = decodeSnapshot(data, entry.Name())
		}
		if err != nil {
			fmt.Fprintf(stderr, "sup: skip %s: %v\n", entry.Name(), err)
			code = 1
			continue
		}
		states = append(states, state)
	}
	if len(states) == 0 {
		fmt.Fprintln(out, "No saved environments.")
		return code, nil
	}
	statuses, statusErr := inventory()
	if statusErr != nil {
		fmt.Fprintf(stderr, "sup: %v; showing unknown status\n", statusErr)
		code = 1
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tREPOSITORY\tSELECTION\tSTATUS\tKITS")
	for _, s := range states {
		status := "unknown"
		if statusErr == nil {
			status = statuses[s.Env.Name]
			if status == "" {
				status = "missing"
			}
		}
		selection := "default"
		if s.Version == 1 {
			selection = "unknown"
		}
		if s.PR != "" {
			selection = "PR #" + s.PR
		} else if s.Ref != "" {
			selection = s.Ref
		}
		if s.Version == 3 {
			parts := []string{}
			for k, v := range s.Args {
				if v != "" {
					parts = append(parts, k+"="+v)
				}
			}
			sort.Strings(parts)
			if len(parts) > 0 {
				selection = strings.Join(parts, ", ")
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", s.Env.Name, s.Repo, display(selection), display(status), len(s.Env.Kits))
	}
	if err = w.Flush(); err != nil {
		return 1, err
	}
	return code, nil
}
func display(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}

func remove(state snapshot, path, dir string, force bool, in io.Reader, out, stderr io.Writer) (int, error) {
	var captured bytes.Buffer
	var flags []string
	if force {
		flags = append(flags, "--force")
	}
	code, err := runSBX("rm", path, in, out, io.MultiWriter(stderr, &captured), flags...)
	if err != nil || code != 0 {
		return code, err
	}
	// sbx env rm currently reports declined confirmation as success + "Aborted.".
	// Preserve state on that path, including when only orphaned credentials remain.
	if strings.Contains(captured.String(), "Aborted.") {
		fmt.Fprintln(out, "Removal cancelled; saved configuration kept.")
		return 0, nil
	}
	statuses, err := inventory()
	if err != nil {
		return 1, fmt.Errorf("saved configuration kept because removal could not be verified: %w", err)
	}
	if _, exists := statuses[state.Env.Name]; exists {
		return 1, errors.New("sandbox still exists; saved configuration kept")
	}
	// Only remove the two files sup owns; never recursively delete unexpected files.
	for _, name := range []string{"sbxenv.yaml", "state.json"} {
		if err = os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return 1, err
		}
	}
	if err = os.Remove(dir); err != nil {
		return 1, fmt.Errorf("sandbox removed, but could not remove state directory: %w", err)
	}
	fmt.Fprintf(out, "Removed %s and its saved configuration.\n", state.Env.Name)
	return 0, nil
}
