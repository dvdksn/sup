package sup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListKitAliases(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("PATH", "")
	path := luaFile(t, `require('sup').setup({
 kits={signing='ghcr.io/example/signing:latest',browser='ghcr.io/example/browser:latest'},
 args={required={required=true}},
 name=function() error('must not run') end,
 defaults=function() error('must not run') end,
 })`)
	var out, stderr bytes.Buffer
	code, err := Run([]string{"kits", "--config", path}, nil, &out, &stderr)
	if code != 0 || err != nil {
		t.Fatal(code, err)
	}
	fields := strings.Fields(out.String())
	if strings.Join(fields, " ") != "NAME SOURCE browser ghcr.io/example/browser:latest signing ghcr.io/example/signing:latest" {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "state")); !os.IsNotExist(err) {
		t.Fatal("created state")
	}
	for _, body := range []string{`return {}`, `return {kits={}}`} {
		out.Reset()
		code, err = Run([]string{"kits", "--config", luaFile(t, body)}, nil, &out, &stderr)
		if code != 0 || err != nil || out.String() != "No kit aliases configured.\n" {
			t.Fatal(code, err, out.String())
		}
	}
	for _, body := range []string{`return {kits={bad=42}}`, `error('broken')`} {
		if _, err := Run([]string{"kits", "--config", luaFile(t, body)}, nil, &out, &stderr); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	for _, args := range [][]string{{"kits", "extra"}, {"kits", "-d"}, {"kits", "--kit", "x"}, {"kits", "-a", "x=y"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
