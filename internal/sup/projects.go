package sup

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
)

//go:embed project.sbxenv.yaml
var projectEnvironment []byte

const projectHelp = `Usage: sup OWNER/REPO|PROJECT [options]
       sup open PROJECT [--via terminal|ssh|herdr] [--agent codex|claude|shell]
       sup ls [--json]
       sup inspect PROJECT
       sup stop PROJECT
       sup rm PROJECT [--force]
       sup recreate PROJECT [--force] [creation options]
       sup history path PROJECT
       sup history clear PROJECT --yes
       sup completion bash

Native project options:
  --native              Select native projects instead of legacy Lua configuration
  --name NAME           Project and sandbox name (default: owner-repo)
  --env-file PATH       Native SBX environment file or directory (repeatable)
  --env-arg KEY=VALUE    Native environment argument (repeatable)
  --ref REF / --pr N    Initial checkout; task worktrees normally live inside the sandbox
  --kit SOURCE          Additional native kit source (repeatable)
  --cwd PATH            Entry directory inside the sandbox
  --via FRONTEND        terminal (default), ssh, or herdr
  --agent AGENT         codex (default), claude, or shell; terminal only
  -d, --detached        Prepare the sandbox and history without attaching
  --plan                Show the native SBX plan without saving a project
  -y, --auto-approve    Pass through SBX plan approval for this invocation
  --no-history          Disable host history persistence for a new project
  --force               Pass through SBX removal approval for rm/recreate

Defaults: ~/.config/sup/sbxenv.yaml, or the bundled dvdksn/kit environment.
Project history survives rm and recreate. Only history clear deletes it.
Existing Lua config and old snapshots still work; use --native to opt in.
`

type projectFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type projectRecord struct {
	Version        int               `json:"version"`
	Name           string            `json:"name"`
	Repo           string            `json:"repo"`
	Files          []projectFile     `json:"files"`
	Args           map[string]string `json:"args"`
	Kits           []string          `json:"kits,omitempty"`
	CWD            string            `json:"cwd"`
	History        string            `json:"history,omitempty"`
	SandboxID      string            `json:"sandboxId,omitempty"`
	Removed        bool              `json:"removed,omitempty"`
	MachineID      string            `json:"machineId,omitempty"`
	HerdrWorkspace string            `json:"herdrWorkspace,omitempty"`
}
type projectOptions struct {
	command, target, name, via, agent, cwd, historyAction      string
	files, kits                                                []string
	args                                                       map[string]string
	detached, plan, force, yes, approve, noHistory, json, help bool
}
type projectRuntime struct {
	root, dataRoot string
	in             io.Reader
	out, stderr    io.Writer
}
type liveSandbox struct {
	Name   string `json:"name"`
	ID     string `json:"id"`
	Status string `json:"status"`
}
type processExit struct{ code int }

func (e processExit) Error() string { return fmt.Sprintf("command exited with status %d", e.code) }

// Lua remains a compatibility route. Native projects do not pass through its schema.
func Run(args []string, in io.Reader, out, stderr io.Writer) (int, error) {
	if len(args) > 0 && args[0] == "__history" {
		if len(args) != 3 || !namePattern.MatchString(args[1]) || !filepath.IsAbs(args[2]) {
			return 1, errors.New("invalid internal history arguments")
		}
		r := projectRuntime{in: in, out: out, stderr: stderr}
		return projectResult(r.history(&projectRecord{Name: args[1], History: args[2]}))
	}
	if len(args) > 0 && (args[0] == "completion" || args[0] == "__complete" || args[0] == "args" || args[0] == "kits") {
		return runLegacy(args, in, out, stderr)
	}
	native := false
	for _, arg := range args {
		switch arg {
		case "--native", "--env-file", "--env-arg", "--via", "--agent", "--cwd", "--no-history", "--json", "--auto-approve", "-y":
			native = true
		}
	}
	if len(args) > 0 {
		switch args[0] {
		case "open", "stop", "recreate", "inspect", "history":
			native = true
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 1, err
	}
	stateRoot := filepath.Join(xdg("XDG_STATE_HOME", filepath.Join(home, ".local", "state")), "sup")
	if !native {
		if options, parseErr := parseProjects(args); parseErr == nil && options.target != "" {
			if _, name, idErr := identity(optionsForNative(options)); idErr == nil {
				if _, err := os.Stat(filepath.Join(stateRoot, name, "project.json")); err == nil {
					native = true
				}
			}
		}
		if len(args) > 0 && args[0] == "ls" {
			entries, _ := os.ReadDir(stateRoot)
			for _, entry := range entries {
				if _, err := os.Stat(filepath.Join(stateRoot, entry.Name(), "project.json")); err == nil {
					native = true
					break
				}
			}
		}
	}
	// An explicit Lua configuration always selects the old interface.
	for _, arg := range args {
		if arg == "--config" {
			return runLegacy(args, in, out, stderr)
		}
	}
	if !native {
		_, luaErr := os.Stat(defaultConfig(home))
		_, envErr := os.Stat(filepath.Join(xdg("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "sup", "sbxenv.yaml"))
		if luaErr == nil && envErr != nil {
			return runLegacy(args, in, out, stderr)
		}
	}
	options, err := parseProjects(args)
	if err != nil {
		return 1, err
	}
	if options.help {
		_, err = io.WriteString(out, projectHelp)
		return projectResult(err)
	}
	r := projectRuntime{root: stateRoot, dataRoot: filepath.Join(xdg("XDG_DATA_HOME", filepath.Join(home, ".local", "share")), "sup", "projects"), in: in, out: out, stderr: stderr}
	if !filepath.IsAbs(r.root) || !filepath.IsAbs(r.dataRoot) {
		return 1, errors.New("XDG state and data roots must be absolute")
	}
	return projectResult(r.run(options, home))
}
func projectResult(err error) (int, error) {
	var exit processExit
	if errors.As(err, &exit) {
		if err.Error() == exit.Error() {
			return exit.code, nil
		}
		return exit.code, err
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}
func parseProjects(args []string) (o projectOptions, err error) {
	o.args = map[string]string{}
	o.via = "terminal"
	o.agent = "codex"
	o.cwd = "/home/agent/workspace"
	if len(args) > 0 {
		switch args[0] {
		case "open", "ls", "inspect", "stop", "rm", "recreate", "history":
			o.command = args[0]
			args = args[1:]
		}
	}
	if o.command == "history" {
		if len(args) == 0 {
			return o, errors.New("usage: sup history path|clear PROJECT")
		}
		o.historyAction = args[0]
		args = args[1:]
		if o.historyAction != "path" && o.historyAction != "clear" {
			return o, errors.New("history action must be path or clear")
		}
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--native":
		case "-h", "--help":
			o.help = true
		case "-d", "--detached":
			o.detached = true
		case "--plan":
			o.plan = true
		case "--force", "-f":
			o.force = true
		case "--yes":
			o.yes = true
		case "--auto-approve", "-y":
			o.approve = true
		case "--no-history":
			o.noHistory = true
		case "--json":
			o.json = true
		case "--env-file", "--env-arg", "--name", "--via", "--agent", "--cwd", "--ref", "--pr", "--kit":
			i++
			if i == len(args) || args[i] == "" || strings.HasPrefix(args[i], "--") {
				return o, fmt.Errorf("%s requires a value", arg)
			}
			if seen[arg] && arg != "--env-file" && arg != "--env-arg" && arg != "--kit" {
				return o, fmt.Errorf("%s specified twice", arg)
			}
			seen[arg] = true
			switch arg {
			case "--env-file":
				o.files = append(o.files, args[i])
			case "--env-arg":
				key, value, ok := strings.Cut(args[i], "=")
				if !ok {
					return o, errors.New("--env-arg requires KEY=VALUE")
				}
				if err := putArg(o.args, key, value); err != nil {
					return o, err
				}
			case "--kit":
				o.kits = append(o.kits, args[i])
			case "--name":
				o.name = args[i]
			case "--via":
				o.via = args[i]
			case "--agent":
				o.agent = args[i]
			case "--cwd":
				o.cwd = args[i]
			case "--ref", "--pr":
				if err := putArg(o.args, strings.TrimPrefix(arg, "--"), args[i]); err != nil {
					return o, err
				}
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return o, fmt.Errorf("unknown option %s", arg)
			}
			if o.target != "" {
				return o, errors.New("expected one repository or project")
			}
			o.target = arg
		}
	}
	if o.help {
		return o, nil
	}
	if o.command == "ls" {
		if o.target != "" || o.name != "" || len(o.files) > 0 || len(o.args) > 0 || len(o.kits) > 0 || o.detached || o.plan || o.force || o.yes || o.approve || o.noHistory || seen["--via"] || seen["--agent"] || seen["--cwd"] {
			return o, errors.New("sup ls only accepts --json")
		}
		return o, nil
	}
	if o.target == "" {
		return o, errors.New(projectHelp)
	}
	if o.via != "terminal" && o.via != "ssh" && o.via != "herdr" {
		return o, errors.New("--via must be terminal, ssh, or herdr")
	}
	if o.agent != "codex" && o.agent != "claude" && o.agent != "shell" {
		return o, errors.New("--agent must be codex, claude, or shell")
	}
	if seen["--agent"] && o.via != "terminal" {
		return o, errors.New("--agent is only available with --via terminal")
	}
	if !strings.HasPrefix(o.cwd, "/") || strings.IndexByte(o.cwd, 0) >= 0 {
		return o, errors.New("--cwd must be an absolute sandbox path")
	}
	if o.force && o.command != "rm" && o.command != "recreate" {
		return o, errors.New("--force is only available for rm/recreate")
	}
	if o.yes && (o.command != "history" || o.historyAction != "clear") {
		return o, errors.New("--yes is only available for history clear")
	}
	if o.command == "history" && o.historyAction == "clear" && !o.yes {
		return o, errors.New("history clear deletes saved conversations; supply --yes")
	}
	if o.json && o.command != "inspect" {
		return o, errors.New("--json is only available for ls/inspect")
	}
	if o.command == "stop" || o.command == "rm" || o.command == "inspect" || o.command == "history" {
		if len(o.files) > 0 || len(o.args) > 0 || len(o.kits) > 0 || o.detached || o.plan || o.approve || o.noHistory || o.name != "" || seen["--via"] || seen["--agent"] || seen["--cwd"] {
			return o, fmt.Errorf("sup %s does not accept creation or attachment options", o.command)
		}
	}
	if o.args["ref"] != "" && o.args["pr"] != "" {
		return o, errors.New("ref and pr are mutually exclusive")
	}
	return o, nil
}
func (r projectRuntime) path(name string) string { return filepath.Join(r.root, name, "project.json") }
func (r projectRuntime) load(name string) (*projectRecord, error) {
	data, err := os.ReadFile(r.path(name))
	if err != nil {
		return nil, err
	}
	var p projectRecord
	if err = json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("invalid project %s: %w", name, err)
	}
	if p.Version != 1 || p.Name != name || !namePattern.MatchString(name) || !repoPattern.MatchString(p.Repo) || !strings.HasPrefix(p.CWD, "/") || len(p.Files) == 0 {
		return nil, errors.New("unsupported or corrupt project record")
	}
	if p.History != "" && (!filepath.IsAbs(p.History) || strings.Contains(p.History, ":")) {
		return nil, errors.New("invalid project history path")
	}
	for _, f := range p.Files {
		if !filepath.IsAbs(f.Path) {
			return nil, errors.New("invalid environment file path")
		}
	}
	if p.Args == nil {
		p.Args = map[string]string{}
	}
	return &p, nil
}
func writePrivate(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateBytes(path, append(data, '\n'))
}
func writePrivateBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sup-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
func (r projectRuntime) save(p *projectRecord) error { return writePrivate(r.path(p.Name), p) }
func (r projectRuntime) lock(name string) (func(), error) {
	dir := filepath.Join(r.root, ".project-locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another operation is managing %s", name)
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
func (r projectRuntime) command(program string, capture bool, args ...string) ([]byte, error) {
	cmd := exec.Command(program, args...)
	cmd.Stdin = r.in
	cmd.Stderr = r.stderr
	var buf bytes.Buffer
	if capture {
		cmd.Stdout = &buf
	} else {
		cmd.Stdout = r.out
	}
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code := exit.ExitCode()
			if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				code = 128 + int(status.Signal())
			}
			return buf.Bytes(), processExit{code}
		}
		return buf.Bytes(), err
	}
	return buf.Bytes(), nil
}
func (r projectRuntime) inventory() (map[string]liveSandbox, error) {
	data, err := r.command("sbx", true, "ls", "--json")
	if err != nil {
		return nil, fmt.Errorf("query sandbox inventory: %w", err)
	}
	var result struct {
		Sandboxes *[]liveSandbox `json:"sandboxes"`
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result.Sandboxes == nil {
		return nil, errors.New("sbx inventory has no sandboxes array")
	}
	items := map[string]liveSandbox{}
	for _, s := range *result.Sandboxes {
		if s.Name == "" || s.ID == "" || s.Status == "" {
			return nil, errors.New("sbx inventory requires sandbox name, ID, and status")
		}
		if _, exists := items[s.Name]; exists {
			return nil, errors.New("duplicate sandbox name in inventory")
		}
		items[s.Name] = s
	}
	return items, nil
}
func (r projectRuntime) check(p *projectRecord, allowMissing bool) (*liveSandbox, error) {
	items, err := r.inventory()
	if err != nil {
		return nil, err
	}
	s, exists := items[p.Name]
	if !exists {
		if p.SandboxID != "" && !allowMissing {
			return nil, fmt.Errorf("sandbox %s is missing; use sup recreate %s to replace it", p.Name, p.Name)
		}
		return nil, nil
	}
	if p.SandboxID == "" || p.SandboxID != s.ID {
		return nil, errors.New("sandbox identity does not match project; refusing to operate on an unrelated sandbox")
	}
	return &s, nil
}
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
	env := map[string]any{"schemaVersion": "1", "name": p.Name}
	if len(p.Kits) > 0 {
		env["kits"] = p.Kits
	}
	if p.History != "" {
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		binary, err = filepath.EvalSymlinks(binary)
		if err != nil {
			return err
		}
		env["env"] = map[string]string{"CODEX_SQLITE_HOME": "/home/agent/project-history/codex/sqlite"}
		env["lifecycle"] = map[string]any{"postCreate": []any{map[string]string{"name": "Attach project conversation history", "command": shellQuote(binary) + " __history " + shellQuote(p.Name) + " " + shellQuote(p.History)}}}
	}
	return writePrivate(path, env)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (r projectRuntime) envArgs(p *projectRecord, verb string, extra ...string) []string {
	args := []string{"env", verb}
	for _, f := range p.Files {
		args = append(args, f.Path)
	}
	args = append(args, filepath.Join(r.root, p.Name, "project.sbxenv.yaml"), "--name", p.Name)
	keys := []string{}
	for key := range p.Args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--env-arg", key+"="+p.Args[key])
	}
	return append(args, extra...)
}
func (r projectRuntime) history(p *projectRecord) error {
	if p.History == "" {
		return nil
	}
	if strings.Contains(p.History, ":") {
		return errors.New("history path cannot contain a colon (SBX mount syntax)")
	}
	info, err := os.Lstat(p.History)
	if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return errors.New("history root must be a real directory")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = os.MkdirAll(p.History, 0700); err != nil {
		return err
	}
	marker := filepath.Join(p.History, ".sup-project.json")
	data, err := os.ReadFile(marker)
	if err == nil {
		var identity struct {
			Project string `json:"project"`
		}
		if err = json.Unmarshal(data, &identity); err != nil || identity.Project != p.Name {
			return errors.New("history belongs to another project")
		}
	} else if os.IsNotExist(err) {
		if err = writePrivate(marker, map[string]string{"project": p.Name}); err != nil {
			return err
		}
	} else {
		return err
	}
	if _, err = r.command("sbx", false, "mount", p.Name, p.History+":/home/agent/project-history:rw"); err != nil {
		return fmt.Errorf("attach history: %w", err)
	}
	if _, err = r.command("sbx", false, "exec", p.Name, "/usr/local/bin/sup-history", p.Name, "/home/agent/project-history"); err != nil {
		return fmt.Errorf("connect agent history (project-history kit required): %w", err)
	}
	return nil
}
func (r projectRuntime) run(o projectOptions, home string) error {
	if o.command == "ls" {
		return r.list(o.json)
	}
	repo, name, err := identity(options{target: o.target, name: o.name})
	if err != nil {
		return err
	}
	unlock, err := r.lock(name)
	if err != nil {
		return err
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	p, loadErr := r.load(name)
	stored := loadErr == nil
	if loadErr != nil && !os.IsNotExist(loadErr) {
		return loadErr
	}
	if !stored {
		if _, err := os.Stat(filepath.Join(r.root, name, "state.json")); err == nil {
			return errors.New("name belongs to a legacy Lua environment; use --config for it or choose another --name")
		}
		if repo == "" || o.command == "stop" || o.command == "rm" || o.command == "recreate" || o.command == "inspect" || o.command == "history" {
			return fmt.Errorf("no saved project named %s", name)
		}
		p = &projectRecord{Version: 1, Name: name, Repo: repo, Args: map[string]string{"repo": repo}, CWD: o.cwd, Kits: o.kits}
		if !o.noHistory {
			p.History = filepath.Join(r.dataRoot, name, "history")
		}
	} else {
		if repo != "" && repo != p.Repo {
			return fmt.Errorf("project belongs to %s", p.Repo)
		}
		if o.command != "recreate" && (len(o.files) > 0 || len(o.kits) > 0 || o.noHistory) {
			return errors.New("project setup is saved; use recreate to change it")
		}
		for key, value := range o.args {
			if key == "repo" && value != p.Repo {
				return errors.New("repo argument must match the project repository")
			}
			if o.command != "recreate" && p.Args[key] != value {
				return fmt.Errorf("argument %s differs; use recreate or another --name", key)
			}
		}
		if o.command != "recreate" && o.cwd != "/home/agent/workspace" && o.cwd != p.CWD {
			return errors.New("entry directory differs; use recreate")
		}
	}
	if !stored || o.command == "recreate" {
		for key, value := range o.args {
			if key == "repo" && value != p.Repo {
				return errors.New("repo argument must match project")
			}
			p.Args[key] = value
		}
		if p.Args["ref"] != "" && p.Args["pr"] != "" {
			return errors.New("ref and pr are mutually exclusive")
		}
		if len(o.kits) > 0 {
			p.Kits = o.kits
		}
		if o.noHistory {
			p.History = ""
		}
		if o.cwd != "/home/agent/workspace" {
			p.CWD = o.cwd
		}
		paths := o.files
		if len(paths) == 0 && len(p.Files) == 0 {
			candidate := filepath.Join(xdg("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "sup", "sbxenv.yaml")
			if _, err := os.Stat(candidate); err == nil {
				paths = []string{candidate}
			} else if !os.IsNotExist(err) {
				return err
			} else {
				base := filepath.Join(r.root, name, "base.sbxenv.yaml")
				if err = writePrivateBytes(base, projectEnvironment); err != nil {
					return err
				}
				paths = []string{base}
			}
		}
		if len(paths) > 0 {
			p.Files = nil
			for _, path := range paths {
				file, err := envFile(path)
				if err != nil {
					return err
				}
				p.Files = append(p.Files, file)
			}
		} else {
			for i, file := range p.Files {
				updated, err := envFile(file.Path)
				if err != nil {
					return err
				}
				p.Files[i] = updated
			}
		}
	}
	overlay := filepath.Join(r.root, name, "project.sbxenv.yaml")
	if o.plan {
		// Use the same directory so relative references and projectDir keep their meaning.
		if err = os.MkdirAll(filepath.Join(r.root, name), 0700); err != nil {
			return err
		}
		temp, err := os.MkdirTemp(filepath.Join(r.root, name), ".plan-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temp)
		path := filepath.Join(temp, "sbxenv.yaml")
		if err = r.overlay(p, path); err != nil {
			return err
		}
		args := r.envArgs(p, "plan")
		for i, arg := range args {
			if arg == overlay {
				args[i] = path
			}
		}
		_, err = r.command("sbx", false, args...)
		return err
	}
	if o.command == "inspect" {
		return json.NewEncoder(r.out).Encode(p)
	}
	if o.command == "history" {
		return r.manageHistory(p, o)
	}
	if o.command == "stop" {
		s, err := r.check(p, false)
		if err != nil {
			return err
		}
		if s == nil {
			return errors.New("sandbox is not provisioned")
		}
		if _, err = r.command("sbx", false, "stop", p.Name); err != nil {
			return err
		}
		return r.manageMachine(p, "disable")
	}
	if o.command == "rm" {
		if err = r.overlay(p, overlay); err != nil {
			return err
		}
		return r.remove(p, o.force)
	}
	if o.command != "recreate" {
		for _, file := range p.Files {
			current, err := envFile(file.Path)
			if err != nil {
				return err
			}
			if current.SHA256 != file.SHA256 {
				return errors.New("environment file changed; use recreate to apply the new setup")
			}
		}
	}
	if o.command == "recreate" {
		// Keep the original manifest and environment for teardown; save replacements only afterward.
		old, err := r.load(name)
		if err != nil {
			return err
		}
		if err = r.remove(old, o.force); err != nil {
			return err
		}
		p.SandboxID = ""
		p.Removed = true
		p.MachineID = ""
		p.HerdrWorkspace = ""
	}
	if _, err = r.check(p, false); err != nil {
		return err
	}
	if err = r.overlay(p, overlay); err != nil {
		return err
	}
	if err = r.save(p); err != nil {
		return err
	}
	extras := []string{"--detached"}
	if o.approve {
		extras = append(extras, "--auto-approve")
	}
	_, runErr := r.command("sbx", false, r.envArgs(p, "run", extras...)...)
	items, err := r.inventory()
	if err != nil {
		return errors.Join(runErr, err)
	}
	if s, exists := items[p.Name]; exists {
		if p.SandboxID != "" && p.SandboxID != s.ID {
			return errors.New("sandbox identity changed during provisioning")
		}
		p.SandboxID = s.ID
		p.Removed = false
		if err = r.save(p); err != nil {
			return err
		}
	} else {
		if runErr != nil {
			return runErr
		}
		return errors.New("SBX did not create/start a sandbox; retry the saved project")
	}
	if runErr != nil {
		return runErr
	}
	if items[p.Name].Status != "running" {
		return errors.New("sandbox did not reach running state; project retained for retry")
	}
	// postCreate runs only once; check again on every open to recover failed setup.
	if err = r.history(p); err != nil {
		return err
	}
	if o.detached {
		fmt.Fprintf(r.out, "Ready: %s\n", p.Name)
		return nil
	}
	if o.via == "herdr" {
		return r.connectHerdr(p)
	}
	unlock()
	unlock = nil
	if o.via == "ssh" {
		if _, err = r.command("sbx", false, "setup", "ssh"); err != nil {
			return err
		}
		_, err = r.command("ssh", false, "-t", p.Name+".sbx", "cd "+shellQuote(p.CWD)+" && exec \"${SHELL:-/bin/bash}\" -il")
		return err
	}
	agent := o.agent
	if agent == "shell" {
		agent = "bash"
	}
	args := []string{"exec", "-it", "--workdir", p.CWD, p.Name, agent}
	if o.agent == "shell" {
		args = append(args, "-il")
	}
	_, err = r.command("sbx", false, args...)
	return err
}
func (r projectRuntime) remove(p *projectRecord, force bool) error {
	if _, err := r.check(p, true); err != nil {
		return err
	}
	if p.MachineID != "" {
		if _, err := r.matchingMachine(p); err != nil {
			return err
		}
	}
	extras := []string{}
	if force {
		extras = append(extras, "--force")
	}
	// Capture cancellation as SBX currently exits zero when a destroy plan is declined.
	var output, errout bytes.Buffer
	rr := r
	rr.out = io.MultiWriter(r.out, &output)
	rr.stderr = io.MultiWriter(r.stderr, &errout)
	if _, err := rr.command("sbx", false, r.envArgs(p, "rm", extras...)...); err != nil {
		return err
	}
	if strings.Contains(output.String()+errout.String(), "Aborted.") {
		return errors.New("removal cancelled; project and history retained")
	}
	items, err := r.inventory()
	if err != nil {
		return err
	}
	if _, exists := items[p.Name]; exists {
		return errors.New("sandbox still exists; project retained")
	}
	if err = r.manageMachine(p, "remove"); err != nil {
		return err
	}
	p.SandboxID = ""
	p.Removed = true
	p.HerdrWorkspace = ""
	if err = r.save(p); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "Removed sandbox %s; project configuration and history retained.\n", p.Name)
	return nil
}
func (r projectRuntime) manageHistory(p *projectRecord, o projectOptions) error {
	if p.History == "" {
		return errors.New("history persistence is disabled for this project")
	}
	if o.historyAction == "path" {
		fmt.Fprintln(r.out, p.History)
		return nil
	}
	s, err := r.check(p, true)
	if err != nil {
		return err
	}
	if s != nil && s.Status != "stopped" {
		return errors.New("stop the sandbox before clearing history")
	}
	// Delete only this project's managed history root; configuration is stored elsewhere.
	if info, err := os.Lstat(p.History); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("history root must be a real directory")
		}
		data, err := os.ReadFile(filepath.Join(p.History, ".sup-project.json"))
		if err != nil {
			return err
		}
		var identity struct {
			Project string `json:"project"`
		}
		if json.Unmarshal(data, &identity) != nil || identity.Project != p.Name {
			return errors.New("history belongs to another project")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.RemoveAll(p.History); err != nil {
		return err
	}
	// A stopped sandbox's saved mount requires its source directory to continue existing.
	if err = os.MkdirAll(p.History, 0700); err != nil {
		return err
	}
	if err = writePrivate(filepath.Join(p.History, ".sup-project.json"), map[string]string{"project": p.Name}); err != nil {
		return err
	}
	fmt.Fprintf(r.out, "Cleared history for %s.\n", p.Name)
	return nil
}
func (r projectRuntime) list(asJSON bool) error {
	entries, err := os.ReadDir(r.root)
	if os.IsNotExist(err) {
		entries = nil
	} else if err != nil {
		return err
	}
	type row struct {
		*projectRecord
		Status string `json:"status"`
	}
	rows := []row{}
	for _, entry := range entries {
		if !entry.IsDir() || !namePattern.MatchString(entry.Name()) {
			continue
		}
		p, err := r.load(entry.Name())
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		rows = append(rows, row{p, "unknown"})
	}
	if len(rows) == 0 {
		if asJSON {
			return json.NewEncoder(r.out).Encode(rows)
		}
		fmt.Fprintln(r.out, "No saved projects.")
		return nil
	}
	items, inventoryErr := r.inventory()
	for i := range rows {
		p := rows[i].projectRecord
		if inventoryErr == nil {
			rows[i].Status = "missing"
			if p.Removed {
				rows[i].Status = "removed"
			}
			if s, exists := items[p.Name]; exists {
				rows[i].Status = s.Status
				if s.ID != p.SandboxID {
					rows[i].Status = "identity-mismatch"
				}
			}
		}
	}
	if asJSON {
		err = json.NewEncoder(r.out).Encode(rows)
	} else {
		w := tabwriter.NewWriter(r.out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PROJECT\tREPOSITORY\tSTATUS\tHISTORY")
		for _, row := range rows {
			history := "off"
			if row.History != "" {
				history = "persistent"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", row.Name, row.Repo, row.Status, history)
		}
		err = w.Flush()
	}
	return errors.Join(err, inventoryErr)
}

func optionsForNative(o projectOptions) options { return options{target: o.target, name: o.name} }
