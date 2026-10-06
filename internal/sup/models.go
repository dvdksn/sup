package sup

import (
	"fmt"
	"io"
)

type projectFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type projectRecord struct {
	Name           string            `json:"name"`
	Repo           string            `json:"repo"`
	Files          []projectFile     `json:"files"`
	Args           map[string]string `json:"args"`
	Kits           []string          `json:"kits,omitempty"`
	CWD            string            `json:"cwd"`
	History        string            `json:"history,omitempty"`
	SandboxID      string            `json:"sandboxId,omitempty"`
	Removed        bool              `json:"removed,omitempty"`
	Ready          bool              `json:"ready"`
	MachineID      string            `json:"machineId,omitempty"`
	HerdrWorkspace string            `json:"herdrWorkspace,omitempty"`
}
type projectOptions struct {
	command, target, name, via, agent, cwd, historyAction      string
	files, kits                                                []string
	args                                                       map[string]string
	detached, plan, force, yes, approve, noHistory, json, help bool
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
