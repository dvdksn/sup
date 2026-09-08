package sup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func managementFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SUP_INVENTORY", `{"sandboxes":[]}`)
	t.Setenv("SUP_RM_EXIT", "0")
	t.Setenv("SUP_LS_EXIT", "0")
	t.Setenv("SUP_ABORT", "")
	log := filepath.Join(root, "args")
	t.Setenv("SUP_ARGS", log)
	script := `#!/bin/sh
if [ "$1" = ls ]; then
 printf '%s' "$SUP_INVENTORY"
 exit "$SUP_LS_EXIT"
fi
printf '%s\n' "$@" > "$SUP_ARGS"
if [ -n "$SUP_ABORT" ]; then printf 'Aborted.\n' >&2; fi
exit "$SUP_RM_EXIT"
`
	if err := os.WriteFile(filepath.Join(bin, "sbx"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "sup"), log
}
func saveFixture(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	s := snapshot{Version: 2, Repo: "docker/docs", PR: "123", Env: Environment{SchemaVersion: "1", Name: name, Agent: "codex", Kits: []Kit{{Source: "registry/browser"}}}}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestList(t *testing.T) {
	root, _ := managementFixture(t)
	var out, stderr bytes.Buffer
	code, err := Run([]string{"ls"}, nil, &out, &stderr)
	if code != 0 || err != nil || !strings.Contains(out.String(), "No saved") {
		t.Fatal(code, err, out.String())
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("empty listing created state directory")
	}
	saveFixture(t, root, "docs-review")
	saveFixture(t, root, "missing-env")
	t.Setenv("SUP_INVENTORY", `{"sandboxes":[{"name":"docs-review","status":"running"},{"name":"unmanaged","status":"stopped"}]}`)
	out.Reset()
	code, err = Run([]string{"ls"}, nil, &out, &stderr)
	if code != 0 || err != nil {
		t.Fatal(code, err, stderr.String())
	}
	for _, want := range []string{"docs-review", "docker/docs", "PR #123", "running", "missing-env", "missing"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "unmanaged") {
		t.Fatal("listed unmanaged sandbox")
	}
	t.Setenv("SUP_LS_EXIT", "7")
	out.Reset()
	stderr.Reset()
	code, err = Run([]string{"ls"}, nil, &out, &stderr)
	if code != 1 || err != nil || !strings.Contains(out.String(), "unknown") || !strings.Contains(stderr.String(), "query sandbox status") {
		t.Fatal(code, err, out.String(), stderr.String())
	}
}
func TestRemove(t *testing.T) {
	for _, tc := range []struct {
		name, abort, exit, inventory string
		keep                         bool
	}{
		{"success", "", "0", `{"sandboxes":[]}`, false},
		{"declined", "yes", "0", `{"sandboxes":[]}`, true},
		{"failed", "", "7", `{"sandboxes":[]}`, true},
		{"still exists", "", "0", `{"sandboxes":[{"name":"docker-docs-pr-123","status":"running"}]}`, true},
		{"invalid inventory", "", "0", `{}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, log := managementFixture(t)
			saveFixture(t, root, "docker-docs-pr-123")
			t.Setenv("SUP_ABORT", tc.abort)
			t.Setenv("SUP_RM_EXIT", tc.exit)
			t.Setenv("SUP_INVENTORY", tc.inventory)
			var out, stderr bytes.Buffer
			code, err := Run([]string{"rm", "docker/docs", "--pr", "123"}, strings.NewReader("n\n"), &out, &stderr)
			_, statErr := os.Stat(filepath.Join(root, "docker-docs-pr-123", "state.json"))
			if (statErr == nil) != tc.keep {
				t.Fatalf("keep=%v, stat=%v; %d %v %s %s", tc.keep, statErr, code, err, out.String(), stderr.String())
			}
			if tc.name == "success" && (code != 0 || err != nil) {
				t.Fatal(code, err)
			}
			data, _ := os.ReadFile(log)
			if !strings.HasPrefix(string(data), "env\nrm\n") || strings.Contains(string(data), "--force") {
				t.Fatalf("wrong args: %s", data)
			}
		})
	}
}
func TestRemoveForceAndMissing(t *testing.T) {
	root, log := managementFixture(t)
	saveFixture(t, root, "docs-review")
	var out, stderr bytes.Buffer
	code, err := Run([]string{"rm", "docs-review", "-f"}, nil, &out, &stderr)
	if code != 0 || err != nil {
		t.Fatal(code, err)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "\n--force\n") {
		t.Fatal("force not passed through")
	}
	os.Remove(log)
	if _, err = Run([]string{"rm", "other/repo"}, nil, &out, &stderr); err == nil {
		t.Fatal("removed unsaved name")
	}
	if _, err = os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("called sbx for unknown environment")
	}
}
func TestManagementOptions(t *testing.T) {
	for _, args := range [][]string{{"ls", "docker/docs"}, {"ls", "--pr", "1"}, {"rm"}, {"rm", "name", "--plan"}, {"rm", "name", "--kit", "browser"}, {"docker/docs", "--force"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
