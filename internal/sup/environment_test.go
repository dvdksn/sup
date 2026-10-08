package sup

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestEnvironmentFetchRejectsInvalidResponses(t *testing.T) {
	for _, scenario := range []struct {
		name, body, want string
		status           int
	}{
		{"not found", "missing file", "404 Not Found: missing file", http.StatusNotFound},
		{"empty", " \n", "empty or oversized", http.StatusOK},
		{"too large", strings.Repeat("x", (1<<20)+1), "empty or oversized", http.StatusOK},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(scenario.status)
				w.Write([]byte(scenario.body))
			}))
			defer server.Close()
			r := projectRuntime{environmentURL: server.URL}
			if _, err := r.fetchEnvironment(); err == nil || !strings.Contains(err.Error(), scenario.want) {
				t.Fatal("unexpected fetch result", err)
			}
		})
	}
}

func TestEnvironmentArguments(t *testing.T) {
	r := projectRuntime{root: "/state"}
	p := &projectRecord{Name: "docker-docs", Repo: "docker/docs"}
	for _, scenario := range []struct {
		verb  string
		extra []string
		want  []string
	}{
		{"run", []string{"--detach", "--force"}, []string{"--detach", "--force", r.envPath(p.Name)}},
		{"plan", nil, []string{r.envPath(p.Name)}},
		{"exec", []string{"--", "true"}, []string{r.envPath(p.Name), "--", "true"}},
		{"rm", []string{"--force"}, []string{"--force", r.envPath(p.Name)}},
	} {
		t.Run(scenario.verb, func(t *testing.T) {
			want := append([]string{"env", scenario.verb, "--name", p.Name, "--env-arg", "repo=docker/docs"}, scenario.want...)
			if got := r.envArgs(p, scenario.verb, scenario.extra...); !reflect.DeepEqual(got, want) {
				t.Fatal("incorrect SBX arguments", got, want)
			}
		})
	}
}
