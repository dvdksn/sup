package sup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

func (r projectRuntime) run(o projectOptions, home string) error {
	if o.command == "ls" {
		return r.list(o.json)
	}
	repo, name, err := identity(o.target, o.name)
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

		if repo == "" || o.command == "stop" || o.command == "rm" || o.command == "recreate" || o.command == "inspect" || o.command == "history" {
			return fmt.Errorf("no saved project named %s", name)
		}
		p = &projectRecord{Name: name, Repo: repo, Args: map[string]string{"repo": repo}, CWD: o.cwd, Kits: o.kits}
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
		p.Ready = false
		p.MachineID = ""
		p.HerdrWorkspace = ""
	}
	live, err := r.check(p, false)
	if err != nil {
		return err
	}
	if live != nil && !p.Ready {
		return errors.New("project creation failed; use recreate to rerun its lifecycle hooks")
	}
	if err = r.overlay(p, overlay); err != nil {
		return err
	}
	if err = r.save(p); err != nil {
		return err
	}
	var runErr error
	if live == nil {
		extras := []string{"--detached"}
		if o.approve {
			extras = append(extras, "--auto-approve")
		}
		_, runErr = r.command("sbx", false, r.envArgs(p, "run", extras...)...)
	} else {
		// Existing machines already have their native setup. env exec starts a
		// stopped machine without repeating host provisioning or its approvals.
		_, runErr = r.command("sbx", false, r.envArgs(p, "exec", "--", "true")...)
	}
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
		if live == nil {
			p.Ready = runErr == nil
		}
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
	args := []string{"exec", "-it", "--workdir", p.CWD, p.Name, "bash"}
	if o.agent == "shell" {
		args = append(args, "-il")
	} else {
		// The custom kit publishes its environment through login-shell profiles.
		// Agent is an enum; cwd is quoted independently of the host command argv.
		args = append(args, "-lc", "cd "+shellQuote(p.CWD)+" && exec "+o.agent)
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
	p.Ready = false
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
	if err = r.clearHistory(p); err != nil {
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
