package sup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Setup is noninteractive. Keep SBX's plan and provisioning chatter in a
// private log, while allowing its stderr diagnostics to reach the terminal.
func (r projectRuntime) setup(p *projectRecord, action string, args ...string) error {
	path := filepath.Join(r.root, p.Name, "setup.log")
	if err := writePrivateBytes(path, nil); err != nil {
		return err
	}
	log, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	fmt.Fprintf(r.stderr, "%s %s…\n", action, p.Name)
	rr := r
	rr.in = nil
	rr.out = log
	rr.stderr = io.MultiWriter(r.stderr, log)
	if r.verbose {
		rr.out = io.MultiWriter(r.out, log)
	}
	_, runErr := rr.command("sbx", false, args...)
	closeErr := log.Close()
	if runErr != nil {
		// Show the end of the native log without replaying the whole plan.
		// The complete output remains available for troubleshooting.
		if !r.verbose {
			if data, err := os.ReadFile(path); err == nil {
				lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
				if len(lines) > 20 {
					lines = lines[len(lines)-20:]
				}
				fmt.Fprintln(r.stderr, strings.Join(lines, "\n"))
			}
		}
		fmt.Fprintf(r.stderr, "%s %s failed. Full SBX output: %s\n", action, p.Name, path)
		return runErr
	}
	return closeErr
}
