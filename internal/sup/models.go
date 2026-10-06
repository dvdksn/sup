package sup

import (
	"fmt"
	"io"
)

type projectRecord struct {
	Name           string `json:"name"`
	Repo           string `json:"repo"`
	History        string `json:"history"`
	SandboxID      string `json:"sandboxId,omitempty"`
	Removed        bool   `json:"removed,omitempty"`
	Ready          bool   `json:"ready"`
	MachineID      string `json:"machineId,omitempty"`
	HerdrWorkspace string `json:"herdrWorkspace,omitempty"`
}
type projectOptions struct {
	command, target, via, agent, historyAction string
	detached, plan, force, yes, json, help     bool
}
type projectRuntime struct {
	root, dataRoot string
	in             io.Reader
	out, stderr    io.Writer
	runner         func(string, bool, ...string) ([]byte, error)
}
type liveSandbox struct {
	Name   string `json:"name"`
	ID     string `json:"id"`
	Status string `json:"status"`
}
type processExit struct{ code int }

func (e processExit) Error() string { return fmt.Sprintf("command exited with status %d", e.code) }
