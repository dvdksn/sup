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
var argNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

func identity(target, override string) (repo, name string, err error) {
	name = target
	if strings.Contains(target, "/") {
		if !repoPattern.MatchString(target) {
			return "", "", errors.New("repository must be OWNER/REPO")
		}
		repo = strings.TrimSuffix(target, ".git")
		if !repoPattern.MatchString(repo) || strings.HasSuffix(repo, "/.") || strings.HasSuffix(repo, "/..") {
			return "", "", errors.New("invalid repository")
		}
		name = strings.ReplaceAll(repo, "/", "-")
		normalized := strings.ReplaceAll(name, "_", "-")
		if normalized != name || len(name) > 100 {
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte(repo)))[:10]
			if len(normalized) > 89 {
				normalized = normalized[:89]
			}
			normalized += "-" + digest
		}
		name = normalized
	}
	if override != "" {
		if repo == "" {
			return "", "", errors.New("--name requires a repository")
		}
		name = override
	}
	if name == "default" || !namePattern.MatchString(name) {
		return "", "", fmt.Errorf("invalid project name: %s", name)
	}
	return repo, name, nil
}

func putArg(args map[string]string, key, value string) error {
	if !argNamePattern.MatchString(key) {
		return fmt.Errorf("invalid argument name: %s", key)
	}
	if _, exists := args[key]; exists {
		return fmt.Errorf("argument %s specified twice", key)
	}
	args[key] = value
	return nil
}

func xdg(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
