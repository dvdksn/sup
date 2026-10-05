package sup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

type snapshot struct {
	Version int               `json:"version"`
	Repo    string            `json:"repo"`
	Ref     string            `json:"ref,omitempty"`
	PR      string            `json:"pr,omitempty"`
	Args    map[string]string `json:"args,omitempty"`
	Env     Environment       `json:"env"`
}

func xdg(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func runLegacy(args []string, in io.Reader, out, stderr io.Writer) (int, error) {
	if len(args) > 0 && args[0] == "completion" {
		if len(args) != 2 || args[1] != "bash" {
			return 1, errors.New("usage: sup completion bash")
		}
		_, err := io.WriteString(out, bashCompletion)
		if err != nil {
			return 1, err
		}
		return 0, nil
	}
	if len(args) > 0 && args[0] == "__complete" {
		return completeNames(args[1:], out)
	}
	o, err := parse(args)
	if err != nil {
		return 1, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		if o.help {
			io.WriteString(out, Help)
			fmt.Fprintf(stderr, "sup: config arguments unavailable: %v\n", err)
			return 0, nil
		}
		return 1, err
	}
	path := o.config
	if path == "" {
		path = defaultConfig(home)
	}
	if o.help {
		return configHelp(path, out, stderr)
	}
	var config *loadedConfig
	defer func() {
		if config != nil {
			config.Close()
		}
	}()
	if o.command == "args" || o.command == "kits" {
		config, err = loadConfig(path)
		if err != nil {
			return 1, err
		}
		if o.command == "kits" {
			return describeKits(config, out)
		}
		return describeArgs(config, out)
	}
	if o.dynamic {
		config, err = loadConfig(path)
		if err != nil {
			return 1, err
		}
		if err = config.validateGiven(o.args); err != nil {
			return 1, err
		}
	}
	root := filepath.Join(xdg("XDG_STATE_HOME", filepath.Join(home, ".local", "state")), "sup")
	if !filepath.IsAbs(root) {
		return 1, errors.New("XDG_STATE_HOME must be an absolute path")
	}
	if o.command == "ls" {
		return list(root, out, stderr)
	}
	repo, name, err := identity(o)
	if err != nil {
		return 1, err
	}
	ctx := ConfigContext{Repo: repo, Name: name}
	// Resolve config-defined names before looking up repository state.
	// Explicit names and saved-name targets bypass config on reconnection.
	if repo != "" && o.name == "" {
		if config == nil {
			config, err = loadConfig(path)
			if err != nil {
				return 1, err
			}
		}
		ctx.Args, err = config.arguments(o.args)
		if err != nil {
			return 1, err
		}
		name, err = config.name(ctx)
		if err != nil {
			return 1, err
		}
		ctx.Name = name
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return 1, err
	}
	if err = os.Chmod(root, 0700); err != nil {
		return 1, err
	}
	dir := filepath.Join(root, name)
	lockRoot := filepath.Join(root, ".locks")
	if err = os.MkdirAll(lockRoot, 0700); err != nil {
		return 1, err
	}
	lock := filepath.Join(lockRoot, name)
	if err = os.Mkdir(lock, 0700); err != nil {
		return 1, fmt.Errorf("cannot lock environment (another sup may be running): %w", err)
	}
	locked := true
	defer func() {
		if locked {
			os.RemoveAll(lock)
		}
	}()
	statePath := filepath.Join(dir, "state.json")
	data, err := os.ReadFile(statePath)
	stored := err == nil
	var state snapshot
	if stored {
		state, err = decodeSnapshot(data, name)
		if err != nil {
			return 1, err
		}
		if repo != "" && repo != state.Repo {
			return 1, fmt.Errorf("name already belongs to %s; choose --name", state.Repo)
		}
		if err = matchArgs(state, o.args); err != nil {
			return 1, err
		}
		if len(o.kits) > 0 || o.config != "" {
			return 1, errors.New("environment configuration is already saved; reconnect without creation flags or choose a new --name")
		}
	} else {
		if !errors.Is(err, os.ErrNotExist) {
			return 1, err
		}
		if repo == "" || o.command == "rm" {
			return 1, fmt.Errorf("no saved environment named %s", name)
		}
		if config == nil {
			config, err = loadConfig(path)
			if err != nil {
				return 1, err
			}
		}
		if ctx.Args == nil {
			ctx.Args, err = config.arguments(o.args)
			if err != nil {
				return 1, err
			}
		}
		env, err := config.environment(ctx, o.kits)
		if err != nil {
			return 1, err
		}
		state = snapshot{Version: 3, Repo: repo, Args: ctx.Args, Env: *env}

	}
	encoded, err := json.MarshalIndent(state.Env, "", "  ")
	if err != nil {
		return 1, err
	}
	encoded = append(encoded, '\n')
	if o.plan {
		path := filepath.Join(lock, "sbxenv.yaml")
		if err = os.WriteFile(path, encoded, 0600); err != nil {
			return 1, err
		}
		return runSBX("plan", path, in, out, stderr)
	}
	if o.command == "rm" {
		path := filepath.Join(dir, "sbxenv.yaml")
		if err = atomicWrite(lock, path, encoded); err != nil {
			return 1, err
		}
		return remove(state, path, dir, o.force, in, out, stderr)
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return 1, err
	}
	if !stored {
		data, err = json.MarshalIndent(state, "", "  ")
		if err != nil {
			return 1, err
		}
		if err = atomicWrite(lock, statePath, append(data, '\n')); err != nil {
			return 1, err
		}
	}
	path = filepath.Join(dir, "sbxenv.yaml")
	if err = atomicWrite(lock, path, encoded); err != nil {
		return 1, err
	}
	if err = os.Remove(lock); err != nil {
		return 1, err
	}
	locked = false
	verb := "run"
	if o.detached {
		verb = "create"
	}
	return runSBX(verb, path, in, out, stderr)
}
func atomicWrite(lock, path string, data []byte) error {
	temp := filepath.Join(lock, filepath.Base(path))
	if err := os.WriteFile(temp, data, 0600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
func runSBX(verb, path string, in io.Reader, out, stderr io.Writer, flags ...string) (int, error) {
	args := append([]string{"env", verb, path}, flags...)
	cmd := exec.Command("sbx", args...)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				return 128 + int(status.Signal()), nil
			}
			return exit.ExitCode(), nil
		}
		return 1, fmt.Errorf("start sbx: %w", err)
	}
	return 0, nil
}

func decodeSnapshot(data []byte, name string) (snapshot, error) {
	var state snapshot
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&state); err != nil {
		return state, fmt.Errorf("invalid saved state: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return state, errors.New("invalid saved state: trailing data")
	}
	if (state.Version != 1 && state.Version != 2 && state.Version != 3) || !repoPattern.MatchString(state.Repo) || state.Env.Name != name {
		return state, errors.New("unsupported or corrupt saved state")
	}
	return state, validate(&state.Env)
}
