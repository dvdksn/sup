package sup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupOutput(t *testing.T) {
	for _, scenario := range []string{"success", "failure", "verbose"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\necho 'ENVIRONMENT PLAN'\ni=0\nwhile [ $i -lt 40 ]; do echo setup-line-$i; i=$((i+1)); done\necho 'Warning: credentials unavailable' >&2\n"
			if scenario == "failure" {
				script += "echo 'error: publisher is not allowed' >&2\nexit 17\n"
			}
			if err := os.WriteFile(filepath.Join(bin, "sbx"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			var out, stderr bytes.Buffer
			r := projectRuntime{root: root, out: &out, stderr: &stderr, verbose: scenario == "verbose"}
			err := r.setup(&projectRecord{Name: "docker-docs"}, "Creating", "env", "run")
			code, _ := projectResult(err)
			if scenario == "failure" && code != 17 || scenario != "failure" && code != 0 {
				t.Fatal("incorrect exit status", code, err)
			}
			if !strings.Contains(stderr.String(), "Warning: credentials unavailable") {
				t.Fatal("setup suppressed a warning", stderr.String())
			}
			if scenario == "verbose" {
				if !strings.Contains(out.String(), "ENVIRONMENT PLAN") {
					t.Fatal("verbose output hid the plan")
				}
			} else if out.Len() != 0 || strings.Contains(stderr.String(), "ENVIRONMENT PLAN") {
				t.Fatal("normal output replayed the environment plan", out.String(), stderr.String())
			}
			if scenario == "failure" && (!strings.Contains(stderr.String(), "publisher is not allowed") || !strings.Contains(stderr.String(), "setup.log")) {
				t.Fatal("missing failure diagnostic or log path", stderr.String())
			}
			path := filepath.Join(root, "docker-docs", "setup.log")
			data, err := os.ReadFile(path)
			if err != nil || !strings.Contains(string(data), "ENVIRONMENT PLAN") || !strings.Contains(string(data), "Warning: credentials unavailable") {
				t.Fatal("incomplete diagnostic log", string(data), err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("diagnostic log is not private", err)
			}
		})
	}
}
