package sup

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const Help = `Usage: sup OWNER/REPO|SAVED-NAME [options]

  -d, --detached      Provision without attaching (sbx env create)
  --kit SOURCE|ALIAS  Add a kit (repeatable, creation only)
  --name NAME        Environment name (default: owner-repo)
  --ref REF          Branch, tag, or commit (creation only)
  --pr NUMBER        Pull request (creation only)
  --config PATH      Config file (default: ~/.config/sup/config.lua)
  --plan             Show sbx's plan without saving or provisioning
  -h, --help         Show this help

Saved environments reuse their configuration. -d does not start an agent task.
`

type options struct {
	target, name, ref, pr, config string
	kits                          []string
	detached, plan, help          bool
}

func parse(args []string) (o options, err error) {
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-h", "--help":
			o.help = true
		case "-d", "--detached":
			o.detached = true
		case "--plan":
			o.plan = true
		case "--kit", "--name", "--ref", "--pr", "--config":
			i++
			if i >= len(args) || args[i] == "" || strings.HasPrefix(args[i], "--") {
				return o, fmt.Errorf("%s requires a value", a)
			}
			if seen[a] && a != "--kit" {
				return o, fmt.Errorf("%s specified twice", a)
			}
			seen[a] = true
			switch a {
			case "--kit":
				o.kits = append(o.kits, args[i])
			case "--name":
				o.name = args[i]
			case "--ref":
				o.ref = args[i]
			case "--pr":
				o.pr = args[i]
			case "--config":
				o.config = args[i]
			}
		default:
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
	if o.target == "" {
		return o, errors.New(Help)
	}
	if o.ref != "" && o.pr != "" {
		return o, errors.New("--ref and --pr are mutually exclusive")
	}
	if o.pr != "" && !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(o.pr) {
		return o, errors.New("--pr must be a positive number")
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
