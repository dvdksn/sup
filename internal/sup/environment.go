package sup

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed project.sbxenv.yaml
var projectEnvironment []byte

func envFile(path string) (projectFile, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return projectFile{}, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return projectFile{}, err
	}
	if info.IsDir() {
		absolute = filepath.Join(absolute, "sbxenv.yaml")
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return projectFile{}, err
	}
	return projectFile{Path: absolute, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}, nil
}
func (r projectRuntime) overlay(p *projectRecord, path string) error {
	// Emit only the fields owned by sup; SBX interprets and merges the files.
	quote := func(value string) string { data, _ := json.Marshal(value); return string(data) }
	var file strings.Builder
	fmt.Fprintf(&file, "schemaVersion: \"1\"\nname: %s\n", quote(p.Name))
	if len(p.Kits) > 0 {
		file.WriteString("kits:\n")
		for _, kit := range p.Kits {
			fmt.Fprintf(&file, "  - %s\n", quote(kit))
		}
	}
	if p.History != "" {
		fmt.Fprintf(&file, "env:\n  CODEX_SQLITE_HOME: %s\n", quote(historyMount+"/codex/sqlite"))
		file.WriteString("lifecycle:\n  postCreate:\n    - name: Mount project history\n      command: |\n")
		for _, line := range strings.Split(strings.TrimSuffix(historyCommand(p.History), "\n"), "\n") {
			fmt.Fprintf(&file, "        %s\n", line)
		}
	}
	return writePrivateBytes(path, []byte(file.String()))
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (r projectRuntime) envArgs(p *projectRecord, verb string, extra ...string) []string {
	// env exec parses flags before its file operands and command separator.
	args := []string{"env", verb, "--name", p.Name}
	keys := []string{}
	for key := range p.Args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--env-arg", key+"="+p.Args[key])
	}
	for _, file := range p.Files {
		args = append(args, file.Path)
	}
	args = append(args, filepath.Join(r.root, p.Name, "project.sbxenv.yaml"))
	return append(args, extra...)
}
