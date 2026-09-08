package sup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeclaredArguments(t *testing.T) {
	path := luaFile(t, `require('sup').setup({args={
  browser={description='Browser engine',default='chromium',choices={'chromium','firefox'}},
  token={required=true}, pr={pattern='[1-9][0-9]*'},
 },name=function(ctx) return ctx.name..'-'..ctx.args.browser end,
 defaults=function(ctx) return {agent='codex',env={TOKEN=ctx.args.token}} end})`)
	c, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	args, err := c.arguments(map[string]string{"token": "a=b:c"})
	if err != nil {
		t.Fatal(err)
	}
	if args["browser"] != "chromium" || args["token"] != "a=b:c" {
		t.Fatal(args)
	}
	name, err := c.name(ConfigContext{Repo: "a/b", Name: "a-b", Args: args})
	if err != nil || name != "a-b-chromium" {
		t.Fatal(name, err)
	}
	for _, given := range []map[string]string{{}, {"token": "x", "other": "y"}, {"token": "x", "browser": "safari"}, {"token": "x", "pr": "0"}} {
		if _, err := c.arguments(given); err == nil {
			t.Fatalf("accepted %v", given)
		}
	}
	for _, body := range []string{
		`return {args={x={required=true,default='x'}}}`,
		`return {args={x={default='x',choices={'y'}}}}`,
		`return {args={x={pattern='['}}}`,
		`return {args={x={default=12}}}`,
		`return {args={x={typo='x'}}}`,
	} {
		if cfg, err := loadConfig(luaFile(t, body)); err == nil {
			cfg.Close()
			t.Fatalf("accepted declaration %s", body)
		}
	}
}
func TestInspectArgsOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("PATH", root)
	path := luaFile(t, `require('sup').setup({
 args={browser={default='chromium',choices={'chromium','firefox'},description='Browser engine'},pr={description='Pull request'}},
 name=function() error('name must not run') end,
 defaults=function() error('defaults must not run') end,
 })`)
	var out, stderr bytes.Buffer
	code, err := Run([]string{"args", "--config", path}, nil, &out, &stderr)
	if code != 0 || err != nil {
		t.Fatal(code, err)
	}
	for _, want := range []string{"browser", "chromium", "firefox", "Browser engine", "pr", "Pull request"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
	if _, err = os.Stat(filepath.Join(root, "state")); !os.IsNotExist(err) {
		t.Fatal("inspection created state")
	}
}
func TestGenericArgCLI(t *testing.T) {
	o, err := parse([]string{"docker/docs", "-a", "one=a=b:c", "--arg", "two="})
	if err != nil || o.args["one"] != "a=b:c" {
		t.Fatal(o, err)
	}
	if v, ok := o.args["two"]; !ok || v != "" {
		t.Fatal(o.args)
	}
	for _, args := range [][]string{{"a/b", "-a", "missing"}, {"a/b", "-a", "=x"}, {"a/b", "-a", "x=1", "--arg", "x=2"}, {"args", "a/b"}, {"args", "-a", "x=1"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestGenericSavedArguments(t *testing.T) {
	root, _ := managementFixture(t)
	config := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "sup", "config.lua")
	body := `require('sup').setup({args={flavor={default='vanilla',choices={'vanilla','chocolate'}},note={}},
 name=function(ctx) return ctx.name..'-'..ctx.args.flavor end,
 defaults=function(ctx) return {agent='codex',env={FLAVOR=ctx.args.flavor,NOTE=ctx.args.note}} end})`
	if err := os.WriteFile(config, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		code, err := Run(args, nil, &out, &stderr)
		if code != 0 || err != nil {
			t.Fatal(args, code, err)
		}
	}
	run("a/b", "--flavor", "chocolate", "--note=a=b:c", "-d")
	data, err := os.ReadFile(filepath.Join(root, "a-b-chocolate", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeSnapshot(data, "a-b-chocolate")
	if err != nil || state.Args["flavor"] != "chocolate" || state.Env.Env["NOTE"] != "a=b:c" {
		t.Fatal(state, err)
	}
	run("a/b", "--flavor", "chocolate")
	if _, err := Run([]string{"a/b", "--name", "a-b-chocolate", "-a", "flavor=vanilla"}, nil, &out, &stderr); err == nil {
		t.Fatal("accepted conflict")
	}
	// An explicit name wins over the callback.
	run("a/b", "--name", "custom", "-d")
	if _, err := os.Stat(filepath.Join(root, "custom", "state.json")); err != nil {
		t.Fatal(err)
	}
	// Name-based operations work without the config or its defaults.
	if err := os.WriteFile(config, []byte(`error('do not load')`), 0600); err != nil {
		t.Fatal(err)
	}
	run("a-b-chocolate", "-a", "flavor=chocolate")
	run("rm", "a-b-chocolate", "-f")
}

func TestConfigAwareHelp(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	path := luaFile(t, `require('sup').setup({args={
  pr={description='Pull request',required=true,pattern='[1-9][0-9]*'},
  browser={description='Browser',default='chromium',choices={'chromium','firefox'}},
 },name=function()error('must not run')end,defaults=function()error('must not run')end})`)
	var out, stderr bytes.Buffer
	code, err := Run([]string{"-h", "--config", path}, nil, &out, &stderr)
	if code != 0 || err != nil || stderr.Len() != 0 {
		t.Fatal(code, err, stderr.String())
	}
	for _, want := range []string{"Usage:", "--name", "Config arguments", "--pr VALUE", "Pull request", "required", "chromium", "firefox"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s: %s", want, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "state")); !os.IsNotExist(err) {
		t.Fatal("help created state")
	}
	for _, broken := range []string{filepath.Join(root, "missing.lua"), luaFile(t, `error('broken config')`)} {
		out.Reset()
		stderr.Reset()
		code, err = Run([]string{"--config", broken, "--help"}, nil, &out, &stderr)
		if code != 0 || err != nil || !strings.Contains(out.String(), "Usage:") || !strings.Contains(stderr.String(), "config arguments unavailable") {
			t.Fatal(code, err, out.String(), stderr.String())
		}
	}
}
func TestDeclaredFlagParsing(t *testing.T) {
	o, err := parse([]string{"--pr", "123", "docker/docs", "--ref=feature/foo", "--note="})
	if err != nil || !o.dynamic || o.args["pr"] != "123" || o.args["ref"] != "feature/foo" {
		t.Fatal(o, err)
	}
	if _, ok := o.args["note"]; !ok {
		t.Fatal("empty flag value lost")
	}
	for _, args := range [][]string{{"a/b", "--pr"}, {"a/b", "--pr", "--help"}, {"a/b", "--pr", "123", "-a", "pr=123"}, {"a/b", "--pr=123", "--pr", "456"}} {
		if _, err := parse(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	for _, key := range []string{"help", "name", "kit", "config", "arg", "detached", "force", "plan"} {
		path := luaFile(t, `return {args={['`+key+`']={}}}`)
		if c, err := loadConfig(path); err == nil {
			c.Close()
			t.Fatalf("accepted reserved key %s", key)
		}
	}
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	path := luaFile(t, `return {args={pr={pattern='[1-9][0-9]*'}},defaults=function()error('must not run')end}`)
	for _, args := range [][]string{{"a/b", "--config", path, "--random", "x"}, {"a/b", "--config", path, "--pr", "oops"}} {
		var out, stderr bytes.Buffer
		if _, err := Run(args, nil, &out, &stderr); err == nil || strings.Contains(err.Error(), "must not run") {
			t.Fatalf("validation failure: %v", err)
		}
	}
}
