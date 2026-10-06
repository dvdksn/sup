package sup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

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
	if p.Name != name || !namePattern.MatchString(name) || !repoPattern.MatchString(p.Repo) {
		return nil, errors.New("invalid project record")
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
	if r.runner != nil {
		return r.runner(program, capture, args...)
	}
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
