package sup

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func TestHerdrReusesProjectWorkspace(t *testing.T) {
	for _, scenario := range []struct {
		name, saved, cwd, want string
		create                 bool
	}{
		{"initial workspace", "", projectDirectory, "w1", false},
		{"saved workspace", "w1", "/tmp", "w1", false},
		{"closed saved workspace", "w9", projectDirectory, "w1", false},
		{"unrelated workspace", "", "/tmp", "w2", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			p := &projectRecord{Name: "docker-docs", Repo: "docker/docs", MachineID: "machine-1", HerdrWorkspace: scenario.saved}
			creates, focuses := 0, 0
			workspaceIDs := []string{"w1"}
			r := projectRuntime{root: root}
			r.runner = func(program string, capture bool, args ...string) ([]byte, error) {
				if !capture || len(args) < 4 || !reflect.DeepEqual(args[:2], []string{"--machine", p.MachineID}) {
					return nil, fmt.Errorf("incorrect machine routing: %s %v", program, args)
				}
				var result any
				switch args[2] + " " + args[3] {
				case "workspace list":
					workspaces := []map[string]string{}
					for _, id := range workspaceIDs {
						workspaces = append(workspaces, map[string]string{"workspace_id": id})
					}
					result = map[string]any{"workspaces": workspaces}
				case "pane list":
					result = map[string]any{"panes": []map[string]string{{"workspace_id": "w1", "cwd": scenario.cwd}}}
				case "workspace create":
					if !reflect.DeepEqual(args[4:], []string{"--cwd", projectDirectory, "--label", p.Name, "--focus"}) {
						return nil, fmt.Errorf("incorrect workspace creation: %v", args)
					}
					creates++
					workspaceIDs = append(workspaceIDs, "w2")
					result = map[string]any{"workspace": map[string]string{"workspace_id": "w2"}}
				case "workspace focus":
					if args[4] != scenario.want {
						return nil, fmt.Errorf("focused wrong workspace: %v", args)
					}
					focuses++
					result = map[string]string{"type": "workspace_info"}
				default:
					return nil, fmt.Errorf("unexpected Herdr call: %v", args)
				}
				return json.Marshal(map[string]any{"result": result})
			}
			for i := 0; i < 2; i++ {
				if err := r.ensureHerdrWorkspace(p); err != nil {
					t.Fatal(err)
				}
			}
			wantCreates, wantFocuses := 0, 2
			if scenario.create {
				wantCreates, wantFocuses = 1, 1
			}
			if creates != wantCreates || focuses != wantFocuses {
				t.Fatal("duplicate workspace or missing focus", creates, focuses)
			}
			saved, err := r.load(p.Name)
			if err != nil || saved.HerdrWorkspace != scenario.want {
				t.Fatal("workspace selection was not saved", err, filepath.Join(root, p.Name))
			}
		})
	}
}
