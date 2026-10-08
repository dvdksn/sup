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
       sup open PROJECT [--via terminal|ssh|herdr] [--agent codex|claude]
       sup ls [--json]
       sup inspect PROJECT           # Prints the saved project as JSON
       sup stop PROJECT
       sup rm PROJECT [--force]
       sup recreate PROJECT [--force] [options]
       sup completion bash

Project options:
  --via FRONTEND        terminal (default), ssh, or herdr
  --agent AGENT         Launch codex or claude instead of Bash; terminal only
  -d, --detach          Prepare without attaching; cannot combine with --via
  --plan                Show the native SBX plan without saving a project
  --verbose             Stream the full SBX setup output
  --force               Skip removal confirmation for rm/recreate

One sandbox per repository; names are derived from owner/repo.
Creation plans are approved automatically. The environment is fetched from dvdksn/kit on creation.
Sessions stay inside the sandbox. Removing or recreating it resets all state.
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
	r := projectRuntime{root: stateRoot, in: in, out: out, stderr: stderr, verbose: options.verbose}
	if !filepath.IsAbs(r.root) {
		return 1, errors.New("XDG_STATE_HOME must be an absolute path")
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
	if len(args) > 0 {
		switch args[0] {
		case "open", "ls", "inspect", "stop", "rm", "recreate":
			o.command = args[0]
			args = args[1:]
		}
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			o.help = true
		case "-d", "--detach":
			o.detached = true
		case "--plan":
			o.plan = true
		case "--force", "-f":
			o.force = true
		case "--verbose":
			o.verbose = true
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
		if o.target != "" || o.detached || o.plan || o.force || o.verbose || seen["--via"] || seen["--agent"] {
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
	if o.detached && seen["--via"] {
		return o, errors.New("--detach cannot be combined with --via; omit --detach to use the selected frontend")
	}
	if seen["--agent"] && o.agent != "codex" && o.agent != "claude" {
		return o, errors.New("--agent must be codex or claude")
	}
	if seen["--agent"] && o.via != "terminal" {
		return o, errors.New("--agent is only available with --via terminal")
	}
	if o.force && o.command != "rm" && o.command != "recreate" {
		return o, errors.New("--force is only available for rm/recreate")
	}
	if o.command == "inspect" && o.verbose {
		return o, errors.New("sup inspect does not accept --verbose")
	}
	if o.json {
		return o, errors.New("--json is only available for ls")
	}
	if o.command == "stop" || o.command == "rm" || o.command == "inspect" {
		if o.detached || o.plan || seen["--via"] || seen["--agent"] {
			return o, fmt.Errorf("sup %s does not accept creation or attachment options", o.command)
		}
	}
	return o, nil
}
