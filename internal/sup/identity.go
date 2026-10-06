package sup

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{1,99}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)

func identity(target string) (repo, name string, err error) {
	name = target
	if strings.Contains(target, "/") {
		if !repoPattern.MatchString(target) {
			return "", "", errors.New("repository must be OWNER/REPO")
		}
		repo = strings.TrimSuffix(strings.ToLower(target), ".git")
		if !repoPattern.MatchString(repo) || strings.HasSuffix(repo, "/.") || strings.HasSuffix(repo, "/..") {
			return "", "", errors.New("invalid repository")
		}
		name = strings.ReplaceAll(repo, "/", "-")
		normalized := strings.ReplaceAll(name, "_", "-")
		// Hyphens in either component make the owner/repo boundary ambiguous.
		if normalized != name || strings.Contains(repo, "-") || len(name) > 100 {
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte(repo)))[:10]
			if len(normalized) > 89 {
				normalized = normalized[:89]
			}
			normalized += "-" + digest
		}
		name = normalized
	}
	if name == "default" || !namePattern.MatchString(name) {
		return "", "", fmt.Errorf("invalid project name: %s", name)
	}
	return repo, name, nil
}

func xdg(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
