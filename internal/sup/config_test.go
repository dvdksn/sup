package sup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func luaFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.lua")
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestExample(t *testing.T) {
	env, err := configure("../../examples/config.lua", ConfigContext{"docker/docs", "docker-docs", "", ""}, []string{"browser"})
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Kits) != 4 || env.Kits[0].Args["repo"] != "docker/docs" || env.SandboxOptions.Memory != "8g" {
		t.Fatalf("unexpected environment: %+v", env)
	}
}
func TestConfigValidation(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"mount", `return {agent='codex',workspace='/tmp'}`, "workspace mounts"},
		{"wrong nested type", `return {agent='codex',sandboxOptions={cpus='4'}}`, "environment.sandboxOptions.cpus: expected integer"},
		{"fraction", `return {agent='codex',sandboxOptions={cpus=1.5}}`, "expected integer"},
		{"unknown field", `return {agent='codex',sandboxOptions={memry='4g'}}`, "sandboxOptions.memry: unknown field"},
		{"kit conflict", `return {agent='codex',kits={{source='x/y',args={v='1'}},{source='x/y',args={v='2'}}}}`, "conflicting arguments"},
		{"sparse list", `return {agent='codex',kits={[2]='x/y'}}`, "contiguous list"},
		{"unknown alias", `return {agent='codex',kits={'typo'}}`, "unknown kit alias"},
		{"relative kit", `return {agent='codex',kits={'./kit'}}`, "absolute path"},
		{"wrong callback result", `return 'bad'`, "environment table"},
		{"name override", `return {agent='codex',name='other'}`, "ctx.name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := luaFile(t, `require('sup').setup({configure=function(ctx) `+tc.body+` end})`)
			_, err := configure(path, ConfigContext{Name: "test"}, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q; got %v", tc.want, err)
			}
		})
	}
}
func TestSetupAndLegacy(t *testing.T) {
	for _, prefix := range []string{"return ", "require('sup').setup("} {
		body := prefix + `{configure=function(ctx) return {agent='codex',kits={},env={},ports={},mcp={servers={}},sandboxOptions={shareSkills=false}} end}`
		if strings.HasSuffix(prefix, "(") {
			body += ")"
		}
		env, err := configure(luaFile(t, body), ConfigContext{Name: "test"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if env.Kits == nil || env.SandboxOptions.ShareSkills == nil || *env.SandboxOptions.ShareSkills {
			t.Fatal("lost empty list or false value")
		}
	}
	for _, body := range []string{`local s=require('sup');s.setup({});s.setup({})`, `require('sup').setup({});return {}`, `require('sup').setup({configure=1})`} {
		if _, err := configure(luaFile(t, body), ConfigContext{Name: "test"}, nil); err == nil {
			t.Fatalf("accepted invalid registration: %s", body)
		}
	}
}
func TestAliasDedupArgsOrder(t *testing.T) {
	p := luaFile(t, `return {configure=function() return {agent='codex',kits={{source='x/y',args={a='1',b='2'}},{source='x/y',args={b='2',a='1'}}}} end}`)
	e, err := configure(p, ConfigContext{Name: "test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Kits) != 1 {
		t.Fatal("not deduplicated")
	}
}
func TestOptions(t *testing.T) {
	o, err := parse([]string{"docker/docs", "--kit", "browser", "--kit", "vale", "-d", "--pr", "123"})
	if err != nil || !o.detached || len(o.kits) != 2 || o.pr != "123" {
		t.Fatalf("%+v %v", o, err)
	}
	for _, a := range [][]string{{"a/b", "--ref", "main", "--pr", "1"}, {"a/b", "--pr", "0"}, {"a/b", "--kit"}, {"a/b", "--wat"}, {"a/b", "--name", "a", "--name", "b"}} {
		if _, err := parse(a); err == nil {
			t.Fatalf("accepted %v", a)
		}
	}
	for _, target := range []string{"../foo", "a/..", "a/.git", "https://github.com/a/b", "../.."} {
		if _, _, err := identity(options{target: target}); err == nil {
			t.Fatalf("accepted %s", target)
		}
	}
}

func TestRepoComposition(t *testing.T) {
	path := luaFile(t, `local sup=require('sup')
 sup.setup({
  kits={browser='registry/browser', task='registry/task'},
  defaults=function(ctx) return {
   agent='codex', kits={'registry/clone'},
   env={SHARED='yes',OVERRIDE='old'},
   sandboxOptions={cpus=4,memory='4g',shareSkills=true},
   ports={{sandbox=3000}},
   secrets={github={command='gh auth token',refresh='55m'}},
  } end,
  repos={['docker/docs']={
   kits={'browser'},env={OVERRIDE='new'},
   sandboxOptions={memory='8g',shareSkills=false},ports={},
   secrets={github={refresh='10m'}},
  }},
 })`)
	env, err := configure(path, ConfigContext{Repo: "docker/docs", Name: "docs"}, []string{"browser", "task"})
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Kits) != 3 || env.Kits[0].Source != "registry/clone" || env.Kits[1].Source != "registry/browser" || env.Kits[2].Source != "registry/task" {
		t.Fatalf("wrong kit order: %+v", env.Kits)
	}
	if env.Env["SHARED"] != "yes" || env.Env["OVERRIDE"] != "new" {
		t.Fatal(env.Env)
	}
	if env.SandboxOptions.CPUs != 4 || env.SandboxOptions.Memory != "8g" || *env.SandboxOptions.ShareSkills {
		t.Fatal(env.SandboxOptions)
	}
	if len(env.Ports) != 0 || env.Secrets["github"].Command != "gh auth token" || env.Secrets["github"].Refresh != "10m" {
		t.Fatal("nested merge or list replacement failed")
	}
	other, err := configure(path, ConfigContext{Repo: "other/repo", Name: "other"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Kits) != 1 || other.SandboxOptions.Memory != "4g" || len(other.Ports) != 1 {
		t.Fatal("unmatched repo changed")
	}
}
func TestDeclarativeDefaults(t *testing.T) {
	path := luaFile(t, `require('sup').setup({defaults={agent='codex'},repos={['docker/docs']={kits={'x/y'}}}})`)
	env, err := configure(path, ConfigContext{Repo: "docker/docs", Name: "docs"}, nil)
	if err != nil || env == nil || len(env.Kits) != 1 {
		t.Fatal(env, err)
	}
}
func TestInvalidComposition(t *testing.T) {
	cases := []string{
		`return {defaults={agent='codex'},configure=function() end}`,
		`return {configure=function() end,repos={}}`,
		`return {defaults=42}`,
		`return {defaults={agent='codex'},repos=42}`,
		`return {defaults={agent='codex'},repos={['docker/docs']=function() end}}`,
		`return {defaults={agent='codex'},repos={['docker/docs']={sandboxOptions={cpus='4'}}}}`,
		`local x={agent='codex'};x.env=x;return {defaults=x}`,
	}
	for _, body := range cases {
		if _, err := configure(luaFile(t, body), ConfigContext{Repo: "docker/docs", Name: "docs"}, nil); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
