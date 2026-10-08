package sup

import (
	"fmt"
	"io"
)

type projectRecord struct {
	Name           string `json:"name"`
	Repo           string `json:"repo"`
	SandboxID      string `json:"sandboxId,omitempty"`
	Ready          bool   `json:"ready"`
	MachineID      string `json:"machineId,omitempty"`
	HerdrWorkspace string `json:"herdrWorkspace,omitempty"`
}
type projectOptions struct {
	command, target, via, agent                string
	detached, plan, force, json, help, verbose bool
}
type projectRuntime struct {
	environmentURL string
	root           string
	in             io.Reader
	out, stderr    io.Writer
	verbose        bool
	runner         func(string, bool, ...string) ([]byte, error)
}
type liveSandbox struct {
	Name   string `json:"name"`
	ID     string `json:"id"`
	Status string `json:"status"`
}
type processExit struct{ code int }

func (e processExit) Error() string { return fmt.Sprintf("command exited with status %d", e.code) }
