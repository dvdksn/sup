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

func (r projectRuntime) run(o projectOptions) error {
	if o.command == "ls" {
		return r.list(o.json)
	}
	repo, name, err := identity(o.target)
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
		if repo == "" || o.command == "stop" || o.command == "rm" || o.command == "recreate" || o.command == "inspect" {
			return fmt.Errorf("no saved project named %s", name)
		}
		p = &projectRecord{Name: name, Repo: repo}
	} else if repo != "" && repo != p.Repo {
		return fmt.Errorf("project belongs to %s", p.Repo)
	}
	if o.plan {
		temp, err := os.MkdirTemp("", "sup-plan-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temp)
		path := filepath.Join(temp, "sbxenv.yaml")
		if err := r.writeEnvironment(p, path); err != nil {
			return err
		}
		_, err = r.command("sbx", false, "env", "plan", "--name", p.Name, path)
		return err
	}
	if o.command == "inspect" {
		return json.NewEncoder(r.out).Encode(p)
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
		return r.remove(p, o.force)
	}
	if o.command == "recreate" {
		// Remove with the environment used to create the existing machine.
		if err = r.remove(p, o.force); err != nil {
			return err
		}
	}
	live, err := r.check(p, false)
	if err != nil {
		return err
	}
	if live != nil && !p.Ready {
		return errors.New("project creation failed; use sup recreate " + p.Name + " to retry")
	}
	if live == nil {
		if err = r.writeEnvironment(p, r.envPath(p.Name)); err != nil {
			return err
		}
	}
	if err = r.save(p); err != nil {
		return err
	}
	var runErr error
	if live == nil {
		runErr = r.setup(p, "Creating", r.envArgs(p, "run", "--detached", "--auto-approve")...)
	} else if live.Status != "running" {
		// Existing machines already have their native setup. env exec starts a
		// stopped machine without repeating host provisioning or its approvals.
		runErr = r.setup(p, "Starting", r.envArgs(p, "exec", "--", "true")...)
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
	if o.via == "terminal" {
		fmt.Fprintf(r.stderr, "Opening %s in %s…\n", o.agent, p.Name)
	} else {
		fmt.Fprintf(r.stderr, "Connecting to %s via %s…\n", p.Name, o.via)
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
		_, err = r.command("ssh", false, "-t", p.Name+".sbx", "cd "+shellQuote(projectDirectory)+" && exec \"${SHELL:-/bin/bash}\" -il")
		return err
	}
	args := []string{"exec", "-it", "--workdir", projectDirectory, p.Name, "bash"}
	if o.agent == "shell" {
		args = append(args, "-il")
	} else {
		// The custom kit publishes its environment through login-shell profiles.
		// Agent is an enum; cwd is quoted independently of the host command argv.
		args = append(args, "-lc", "cd "+shellQuote(projectDirectory)+" && exec "+o.agent)
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
	var removeErr error
	if force {
		removeErr = r.setup(p, "Removing", r.envArgs(p, "rm", extras...)...)
	} else {
		_, removeErr = rr.command("sbx", false, r.envArgs(p, "rm", extras...)...)
	}
	if removeErr != nil {
		return removeErr
	}
	if strings.Contains(output.String()+errout.String(), "Aborted.") {
		return errors.New("removal cancelled; sandbox retained")
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
	p.Ready = false
	p.HerdrWorkspace = ""
	if err = os.RemoveAll(filepath.Dir(r.path(p.Name))); err != nil {
		return fmt.Errorf("remove saved project %s: %w", p.Name, err)
	}
	fmt.Fprintf(r.out, "Removed %s.\n", p.Name)
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
		fmt.Fprintln(w, "PROJECT\tREPOSITORY\tSTATUS")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\n", row.Name, row.Repo, row.Status)
		}
		err = w.Flush()
	}
	return errors.Join(err, inventoryErr)
}
