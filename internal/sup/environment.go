package sup

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const projectDirectory = "/home/agent/workspace"
const projectEnvironmentURL = "https://raw.githubusercontent.com/dvdksn/kit/main/sbxenv.yaml"

func (r projectRuntime) envPath(name string) string {
	return filepath.Join(r.root, name, "sbxenv.yaml")
}

func (r projectRuntime) fetchEnvironment() ([]byte, error) {
	url := r.environmentURL
	if url == "" {
		url = projectEnvironmentURL
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch environment from %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("fetch environment from %s: %s: %s", url, response.Status, strings.TrimSpace(string(body)))
	}
	const maxSize = 1 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read environment from %s: %w", url, err)
	}
	if len(data) > maxSize || len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("fetch environment from %s: empty or oversized environment", url)
	}
	return data, nil
}

func (r projectRuntime) envArgs(p *projectRecord, verb string, extra ...string) []string {
	return r.envArgsAt(p, verb, r.envPath(p.Name), extra...)
}

func (r projectRuntime) envArgsAt(p *projectRecord, verb, path string, extra ...string) []string {
	args := []string{"env", verb, "--name", p.Name, "--env-arg", "repo=" + p.Repo}
	if verb == "exec" {
		return append(append(args, path), extra...)
	}
	return append(append(args, extra...), path)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
