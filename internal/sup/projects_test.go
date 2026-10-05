package sup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyNamesSurviveNativeProjectsAndMissingConfig(t *testing.T) {
	root, _ := managementFixture(t)
	saveFixture(t, root, "old-docs")
	if err := os.Remove(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sup", "config.lua")); err != nil {
		t.Fatal(err)
	}
	native := projectRecord{Version: 1, Name: "new-docs", Repo: "docker/docs", Files: []projectFile{{Path: "/custom/sbxenv.yaml"}}, Args: map[string]string{"repo": "docker/docs"}, CWD: "/home/agent/workspace"}
	if err := (projectRuntime{root: root}).save(&native); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code, err := Run([]string{"old-docs"}, nil, &out, &stderr); err != nil || code != 0 {
		t.Fatal(code, err, stderr.String())
	}
	out.Reset()
	if code, err := Run([]string{"ls", "--legacy"}, nil, &out, &stderr); err != nil || code != 0 || !strings.Contains(out.String(), "old-docs") {
		t.Fatal(code, err, out.String(), stderr.String())
	}
}
