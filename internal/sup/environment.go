package sup

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"path/filepath"
	"strings"
	"text/template"
)

const projectDirectory = "/home/agent/workspace"

//go:embed project.sbxenv.yaml
var projectEnvironment string

func (r projectRuntime) envPath(name string) string {
	return filepath.Join(r.root, name, "sbxenv.yaml")
}

func (r projectRuntime) writeEnvironment(p *projectRecord, path string) error {
	// Render project identity; SBX interprets the native schema.
	funcs := template.FuncMap{
		"quote": func(value string) string { data, _ := json.Marshal(value); return string(data) },
	}
	env, err := template.New("sbxenv").Delims("[[", "]]").Funcs(funcs).Parse(projectEnvironment)
	if err != nil {
		return err
	}
	var file bytes.Buffer
	if err := env.Execute(&file, p); err != nil {
		return err
	}
	return writePrivateBytes(path, file.Bytes())
}

func (r projectRuntime) envArgs(p *projectRecord, verb string, extra ...string) []string {
	args := []string{"env", verb, "--name", p.Name}
	if verb == "exec" {
		return append(append(args, r.envPath(p.Name)), extra...)
	}
	return append(append(args, extra...), r.envPath(p.Name))
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
