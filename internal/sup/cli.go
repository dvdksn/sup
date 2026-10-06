package sup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const projectHelp = `Usage: sup OWNER/REPO|PROJECT [options]
       sup open PROJECT [--via terminal|ssh|herdr] [--agent codex|claude|shell]
       sup ls [--json]
       sup inspect PROJECT
       sup stop PROJECT
       sup rm PROJECT [--force]
       sup recreate PROJECT [--force] [options]
       sup history path PROJECT
       sup history clear PROJECT --yes
       sup completion bash

Project options:
  --via FRONTEND        terminal (default), ssh, or herdr
  --agent AGENT         codex (default), claude, or shell; terminal only
  -d, --detached        Prepare the sandbox and history without attaching
  --plan                Show the native SBX plan without saving a project
  --force               Pass through SBX removal approval for rm/recreate

One sandbox per repository; names are derived from owner/repo.
Creation plans are approved automatically. The environment is embedded in sup.
Project history survives rm and recreate. Only history clear deletes it.
`

func Run(args []string, in io.Reader, out, stderr io.Writer) (int, error) {
	if len(args) > 0 && args[0] == "completion" {
		if len(args) != 2 || args[1] != "bash" {
			return 1, errors.New("usage: sup completion bash")
		}
		_, err := io.WriteString(out, bashCompletion)
		return projectResult(err)
	}
	if len(args) > 0 && args[0] == "__complete" {
		return completeNames(args[1:], out)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 1, err
	}
	stateRoot := filepath.Join(xdg("XDG_STATE_HOME", filepath.Join(home, ".local", "state")), "sup")
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
	return projectResult(r.run(options))
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
	o.via = "terminal"
	o.agent = "codex"
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
		case "--json":
			o.json = true
		case "--via", "--agent":
			i++
			if i == len(args) || args[i] == "" || strings.HasPrefix(args[i], "--") {
				return o, fmt.Errorf("%s requires a value", arg)
			}
			if seen[arg] {
				return o, fmt.Errorf("%s specified twice", arg)
			}
			seen[arg] = true
			switch arg {
			case "--via":
				o.via = args[i]
			case "--agent":
				o.agent = args[i]
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
		if o.target != "" || o.detached || o.plan || o.force || o.yes || seen["--via"] || seen["--agent"] {
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
		if o.detached || o.plan || seen["--via"] || seen["--agent"] {
			return o, fmt.Errorf("sup %s does not accept creation or attachment options", o.command)
		}
	}
	return o, nil
}
