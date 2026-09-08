package sup

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const Help = `Usage: sup OWNER/REPO|SAVED-NAME [options]
       sup ls
       sup args [--config PATH]
       sup rm OWNER/REPO|SAVED-NAME [-a KEY=VALUE] [--name NAME] [-f]

Commands:
  args               Show arguments declared by the Lua config
  ls                 List saved environments and live sandbox status
  rm                 Remove through sbx, then forget saved state

Options:
  -d, --detached      Provision without attaching (sbx env create)
  --kit SOURCE|ALIAS  Add a kit (repeatable, creation only)
  --name NAME        Override the derived environment name
  -a, --arg KEY=VALUE Pass a declared config argument (repeatable)
  --config PATH      Config file (default: ~/.config/sup/config.lua)
  --plan             Show sbx's plan without saving or provisioning
  -f, --force        Skip sbx removal confirmation (rm only)
  -h, --help         Show this help

Saved environments reuse their configuration. -d does not start an agent task.
`

type options struct {
	target, name, config, command        string
	kits                                 []string
	args                                 map[string]string
	detached, plan, help, force, dynamic bool
}

func parse(args []string) (o options, err error) {
	if len(args) > 0 && (args[0] == "ls" || args[0] == "rm" || args[0] == "args") {
		o.command = args[0]
		args = args[1:]
	}
	o.args = map[string]string{}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-h", "--help":
			o.help = true
		case "-d", "--detached":
			o.detached = true
		case "-f", "--force":
			o.force = true
		case "--plan":
			o.plan = true
		case "--kit", "--name", "--config", "-a", "--arg":
			i++
			if i >= len(args) || args[i] == "" || strings.HasPrefix(args[i], "--") {
				return o, fmt.Errorf("%s requires a value", a)
			}
			if seen[a] && a != "--kit" && a != "-a" && a != "--arg" {
				return o, fmt.Errorf("%s specified twice", a)
			}
			seen[a] = true
			switch a {
			case "--kit":
				o.kits = append(o.kits, args[i])
			case "--name":
				o.name = args[i]
			case "--config":
				o.config = args[i]
			case "-a", "--arg":
				key, value, ok := strings.Cut(args[i], "=")
				if !ok {
					return o, errors.New("--arg requires KEY=VALUE")
				}
				if err = putArg(o.args, key, value); err != nil {
					return o, err
				}
			}
		default:
			if strings.HasPrefix(a, "--") {
				key, value, inline := strings.Cut(strings.TrimPrefix(a, "--"), "=")
				if reservedArg(key) {
					return o, fmt.Errorf("--%s must use launcher option syntax", key)
				}
				if !inline {
					i++
					if i >= len(args) || strings.HasPrefix(args[i], "--") {
						return o, fmt.Errorf("--%s requires a value", key)
					}
					value = args[i]
				}
				if err = putArg(o.args, key, value); err != nil {
					return o, err
				}
				o.dynamic = true
				continue
			}
			if strings.HasPrefix(a, "-") {
				return o, fmt.Errorf("unknown option: %s", a)
			}
			if o.target != "" {
				return o, errors.New("expected one repository or saved name")
			}
			o.target = a
		}
	}
	if o.help {
		return o, nil
	}
	if o.command == "ls" {
		if o.target != "" || o.name != "" || len(o.args) > 0 || o.config != "" || len(o.kits) > 0 || o.detached || o.plan || o.force {
			return o, errors.New("sup ls takes no arguments")
		}
		return o, nil
	}
	if o.command == "args" {
		if o.target != "" || o.name != "" || len(o.args) > 0 || len(o.kits) > 0 || o.detached || o.plan || o.force {
			return o, errors.New("sup args only accepts --config")
		}
		return o, nil
	}
	if o.force && o.command != "rm" {
		return o, errors.New("--force is only supported by sup rm")
	}
	if o.command == "rm" && (o.config != "" || len(o.kits) > 0 || o.detached || o.plan) {
		return o, errors.New("sup rm does not accept creation options or --plan")
	}
	if o.target == "" {
		return o, errors.New(Help)
	}
	return o, nil
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{1,99}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func identity(o options) (repo, name string, err error) {
	if strings.Contains(o.target, "/") {
		if !repoPattern.MatchString(o.target) {
			return "", "", errors.New("repository must be OWNER/REPO")
		}
		repo = strings.TrimSuffix(o.target, ".git")
		for _, part := range strings.Split(repo, "/") {
			if part == "" || part == "." || part == ".." {
				return "", "", errors.New("invalid repository")
			}
		}
	}
	name = o.name
	if name == "" {
		name = o.target
		if repo != "" {
			name = strings.ReplaceAll(repo, "/", "-")
			name = safeDerivedName(name)
		}
	}
	if name == "default" || !namePattern.MatchString(name) {
		return "", "", fmt.Errorf("invalid environment name: %s", name)
	}
	if repo == "" && o.name != "" {
		return "", "", errors.New("--name requires a repository target")
	}
	return repo, name, nil
}

// Keep ordinary names readable. Hash any lossy conversion so refs like
// feature/foo and feature-foo cannot silently target the same environment.
func safeDerivedName(raw string) string {
	name := regexp.MustCompile(`[^A-Za-z0-9.-]+`).ReplaceAllString(raw, "-")
	name = strings.TrimLeft(name, ".-")
	if name == "" {
		name = "env"
	}
	if name != raw || len(name) > 100 {
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))[:10]
		if len(name) > 89 {
			name = name[:89]
		}
		name += "-" + digest
	}
	return name
}

var argNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

func putArg(args map[string]string, key, value string) error {
	if !argNamePattern.MatchString(key) {
		return fmt.Errorf("invalid argument name: %s", key)
	}
	if _, exists := args[key]; exists {
		return fmt.Errorf("argument %s specified twice", key)
	}
	args[key] = value
	return nil
}

func reservedArg(key string) bool {
	switch key {
	case "help", "detached", "force", "plan", "kit", "name", "config", "arg":
		return true
	}
	return false
}
