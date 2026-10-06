package sup

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRemovalConfirmation(t *testing.T) {
	for _, command := range []string{"rm", "recreate"} {
		for _, answer := range []string{"\n", "n\n", "", "yes\n"} {
			t.Run(command+"/"+answer, func(t *testing.T) {
				f := newProjectFixture(t)
				f.start(t)
				original := f.project(t).SandboxID
				before := len(f.calls)
				var out bytes.Buffer
				f.runtime.out = &out
				f.runtime.stderr = &out
				f.runtime.in = strings.NewReader(answer)
				args := []string{command, "docker/docs"}
				if command == "recreate" {
					args = append(args, "-d")
				}
				if err := f.run(t, args...); err != nil {
					t.Fatal(err)
				}
				if strings.Count(out.String(), "[y/N]") != 1 || !strings.Contains(out.String(), "files and sessions") {
					t.Fatal("expected one Sup confirmation", out.String())
				}
				removed := false
				for _, call := range f.calls[before:] {
					if len(call) > 2 && call[1] == "env" && call[2] == "rm" {
						removed = true
						if !strings.Contains(strings.Join(call, " "), "--force") {
							t.Fatal("SBX could ask for a second confirmation", call)
						}
					}
				}
				if answer != "yes\n" {
					if removed || f.next != 1 || f.project(t).SandboxID != original || !strings.Contains(out.String(), "Cancelled.") {
						t.Fatal("declining confirmation changed the project", out.String())
					}
				} else if !removed {
					t.Fatal("confirmed removal did not run")
				} else if command == "rm" {
					if _, err := os.Stat(f.runtime.path("docker-docs")); !os.IsNotExist(err) {
						t.Fatal("confirmed removal retained the record", err)
					}
				} else if f.next != 2 || f.project(t).SandboxID == original {
					t.Fatal("confirmed recreation did not replace the sandbox")
				}
			})
		}
	}
}

func TestForcedRemovalSkipsConfirmation(t *testing.T) {
	f := newProjectFixture(t)
	f.start(t)
	var out bytes.Buffer
	f.runtime.stderr = &out
	if err := f.run(t, "rm", "docker/docs", "--force"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "[y/N]") {
		t.Fatal("--force prompted", out.String())
	}
}
