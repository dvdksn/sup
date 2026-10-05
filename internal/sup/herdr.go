package sup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type herdrMachine struct {
	ID      string `json:"id"`
	Target  string `json:"target"`
	Session string `json:"session"`
	Enabled bool   `json:"enabled"`
}

func herdrBinary() string {
	if path := os.Getenv("HERDR_BIN_PATH"); path != "" {
		return path
	}
	return "herdr"
}
func (r projectRuntime) matchingMachine(p *projectRecord) (*herdrMachine, error) {
	data, err := r.command(herdrBinary(), true, "machine", "list", "--json")
	if err != nil {
		return nil, err
	}
	var machines []herdrMachine
	if err = json.Unmarshal(data, &machines); err != nil {
		return nil, err
	}
	var found *herdrMachine
	for _, m := range machines {
		if m.ID == "" || m.Target == "" || m.Session == "" {
			return nil, errors.New("invalid Herdr machine inventory")
		}
		if p.MachineID == m.ID && (m.Target != p.Name+".sbx" || m.Session != "default") {
			return nil, errors.New("saved Herdr machine points somewhere else")
		}
		if m.Target == p.Name+".sbx" && m.Session == "default" {
			if found != nil {
				return nil, errors.New("multiple Herdr machines target this project")
			}
			copy := m
			found = &copy
		}
	}
	return found, nil
}

// A saved profile is runtime state. Remove it on sandbox removal so native
// machine add can install Herdr again after recreation without duplicate profiles.
func (r projectRuntime) manageMachine(p *projectRecord, verb string) error {
	if p.MachineID == "" {
		return nil
	}
	m, err := r.matchingMachine(p)
	if err != nil {
		return err
	}
	if m == nil {
		p.MachineID = ""
		return r.save(p)
	}
	if m.ID != p.MachineID {
		return errors.New("Herdr machine identity changed")
	}
	if _, err = r.command(herdrBinary(), false, "machine", verb, m.ID); err != nil {
		return err
	}
	if verb == "remove" {
		p.MachineID = ""
		p.HerdrWorkspace = ""
		return r.save(p)
	}
	return nil
}
func (r projectRuntime) remote(p *projectRecord, args ...string) (json.RawMessage, error) {
	data, err := r.command(herdrBinary(), true, append([]string{"--machine", p.MachineID}, args...)...)
	if err != nil {
		return nil, err
	}
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err = json.Unmarshal(data, &response); err != nil {
		return nil, err
	}
	if len(response.Error) > 0 && string(response.Error) != "null" {
		return nil, fmt.Errorf("Herdr: %s", response.Error)
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return nil, errors.New("Herdr response is missing its result")
	}
	return response.Result, nil
}
func (r projectRuntime) connectHerdr(p *projectRecord) error {
	if _, err := r.command("sbx", false, "setup", "ssh"); err != nil {
		return err
	}
	if _, err := r.command("sbx", false, "exec", p.Name, "sh", "-lc", configureRemoteShell); err != nil {
		return err
	}
	m, err := r.matchingMachine(p)
	if err != nil {
		return err
	}
	if m == nil {
		fmt.Fprintln(r.out, "Preparing Herdr; its native remote installation may ask for approval.")
		if _, err = r.command(herdrBinary(), false, "machine", "add", p.Name+".sbx", "--label", p.Name, "--remote-session", "default"); err != nil {
			return err
		}
		m, err = r.matchingMachine(p)
		if err != nil {
			return err
		}
		if m == nil {
			return errors.New("Herdr did not save a machine")
		}
	}
	p.MachineID = m.ID
	if err = r.save(p); err != nil {
		return err
	}
	if !m.Enabled {
		if _, err = r.command(herdrBinary(), false, "machine", "enable", m.ID); err != nil {
			return err
		}
	}
	if _, err = r.command("sbx", false, "exec", p.Name, "sh", "-lc", startServer); err != nil {
		return fmt.Errorf("start remote Herdr: %w", err)
	}
	if _, err = r.remote(p, "server", "reload-config"); err != nil {
		return err
	}
	// Install the supported agent hooks inside this machine, where the sessions run.
	script := `set -eu
herdr_bin=$(command -v herdr || true)
if [ -z "$herdr_bin" ]; then herdr_bin="$HOME/.local/bin/herdr"; fi
"$herdr_bin" integration install codex
"$herdr_bin" integration install claude
`
	if _, err = r.command("sbx", false, "exec", p.Name, "sh", "-lc", script); err != nil {
		return fmt.Errorf("install remote agent integrations: %w", err)
	}
	data, err := r.remote(p, "workspace", "list")
	if err != nil {
		return err
	}
	var list struct {
		Workspaces []struct {
			ID    string `json:"workspace_id"`
			Label string `json:"label"`
		} `json:"workspaces"`
	}
	if err = json.Unmarshal(data, &list); err != nil {
		return err
	}
	found := false
	for _, w := range list.Workspaces {
		if w.ID == p.HerdrWorkspace && p.HerdrWorkspace != "" {
			found = true
		}
	}
	if !found {
		data, err = r.remote(p, "workspace", "create", "--cwd", p.CWD, "--label", p.Name, "--focus")
		if err != nil {
			return err
		}
		var created struct {
			Workspace struct {
				ID string `json:"workspace_id"`
			} `json:"workspace"`
		}
		if err = json.Unmarshal(data, &created); err != nil {
			return err
		}
		if created.Workspace.ID == "" {
			return errors.New("Herdr created no workspace ID")
		}
		p.HerdrWorkspace = created.Workspace.ID
		if err = r.save(p); err != nil {
			return err
		}
	}
	fmt.Fprintf(r.out, "Ready: %s. Select its machine in Herdr and run codex or claude in its panes.\n", p.Name)
	return nil
}

const configureRemoteShell = `set -eu
user_name=$(id -un)
if command -v getent >/dev/null 2>&1; then
  login_shell=$(getent passwd "$user_name" | awk -F: 'NR == 1 { print $7 }')
else
  login_shell=$(awk -F: -v user="$user_name" '$1 == user { print $7; exit }' /etc/passwd)
fi
case "$login_shell" in
  /*) test -x "$login_shell" || exit 0 ;;
  *) exit 0 ;;
esac
case "$login_shell" in
  *\"*|*\\*) exit 0 ;;
esac
config_file=${HERDR_CONFIG_PATH:-${XDG_CONFIG_HOME:-$HOME/.config}/herdr/config.toml}
mkdir -p "$(dirname "$config_file")"
if [ ! -e "$config_file" ]; then
  umask 077
  printf '[terminal]\ndefault_shell = "%s"\n' "$login_shell" > "$config_file"
  exit 0
fi
# Preserve explicit user choices and every other section of the config file.
if awk '
  /^\[terminal\][[:space:]]*(#.*)?$/ { terminal = 1; next }
  /^\[/ { terminal = 0 }
  terminal && /^[[:space:]]*default_shell[[:space:]]*=/ {
    value = $0
    sub(/^[^=]*=[[:space:]]*/, "", value)
    sub(/[[:space:]]*(#.*)?$/, "", value)
    if (value != "\"\"") found = 1
  }
  END { exit !found }
' "$config_file"; then
  exit 0
fi
tmp_file=$(mktemp "${config_file}.XXXXXX")
trap 'rm -f "$tmp_file"' EXIT HUP INT TERM
awk -v shell="$login_shell" '
  /^\[terminal\][[:space:]]*(#.*)?$/ {
    print
    print "default_shell = \"" shell "\""
    inserted = 1
    terminal = 1
    next
  }
  /^\[/ { terminal = 0 }
  terminal && /^[[:space:]]*default_shell[[:space:]]*=[[:space:]]*""[[:space:]]*(#.*)?$/ { next }
  { print }
  END {
    if (!inserted) {
      print ""
      print "[terminal]"
      print "default_shell = \"" shell "\""
    }
  }
' "$config_file" > "$tmp_file"
chmod --reference="$config_file" "$tmp_file"
mv "$tmp_file" "$config_file"
trap - EXIT HUP INT TERM
`

const startServer = `set -eu
herdr_bin=$(command -v herdr || true)
if [ -z "$herdr_bin" ]; then herdr_bin="$HOME/.local/bin/herdr"; fi
test -x "$herdr_bin"
unset HERDR_SOCKET_PATH HERDR_PANE_ID HERDR_WORKSPACE_ID HERDR_TAB_ID
if "$herdr_bin" --session default workspace list >/dev/null 2>&1; then exit 0; fi
mkdir -p "$HOME/.local/state/herdr-sandboxes"
nohup "$herdr_bin" --session default server >"$HOME/.local/state/herdr-sandboxes/server.log" 2>&1 </dev/null &
i=0
while [ "$i" -lt 50 ]; do
  if "$herdr_bin" --session default workspace list >/dev/null 2>&1; then exit 0; fi
  i=$((i + 1))
  sleep 0.1
done
echo 'Herdr did not start; see ~/.local/state/herdr-sandboxes/server.log inside the environment' >&2
exit 1
`
