package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// endpoint is a context's docker endpoint.
type endpoint struct {
	Host          string
	SkipTLSVerify bool
}

// contextMeta is the part of a context's meta.json that muscles needs.
type contextMeta struct {
	Endpoints struct {
		Docker endpoint `json:"docker"`
	}
}

// applyContext points the Docker client at the active Docker context's
// daemon, the way docker does: docker doesn't pass the endpoint to
// plugins, and the client muscles uses only reads DOCKER_HOST and friends.
func applyContext(e env) error {
	if e.getenv("DOCKER_HOST") != "" {
		return nil
	}
	dir := e.getenv("DOCKER_CONFIG")
	if dir == "" {
		home, err := e.home()
		if err != nil {
			return fmt.Errorf("finding the Docker config directory: %w", err)
		}
		dir = filepath.Join(home, ".docker")
	}
	name := e.getenv("DOCKER_CONTEXT")
	if name == "" {
		var err error
		if name, err = currentContext(dir); err != nil {
			return err
		}
	}
	if name == "" || name == "default" {
		return nil
	}
	sum := sha256.Sum256([]byte(name))
	id := hex.EncodeToString(sum[:])
	ep, err := readEndpoint(filepath.Join(dir, "contexts", "meta", id, "meta.json"))
	if err != nil {
		return fmt.Errorf("docker context %q: %w", name, err)
	}
	if err := e.setenv("DOCKER_HOST", ep.Host); err != nil {
		return fmt.Errorf("docker context %q: %w", name, err)
	}
	tls := filepath.Join(dir, "contexts", "tls", id, "docker")
	if _, err := os.Stat(tls); err != nil {
		return nil
	}
	if err := e.setenv("DOCKER_CERT_PATH", tls); err != nil {
		return fmt.Errorf("docker context %q: %w", name, err)
	}
	if !ep.SkipTLSVerify {
		if err := e.setenv("DOCKER_TLS_VERIFY", "1"); err != nil {
			return fmt.Errorf("docker context %q: %w", name, err)
		}
	}
	return nil
}

// currentContext reads currentContext from dir/config.json; a missing file
// means the default context.
func currentContext(dir string) (string, error) {
	path := filepath.Join(dir, "config.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg struct {
		CurrentContext string `json:"currentContext"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return cfg.CurrentContext, nil
}

// readEndpoint reads a context's docker endpoint from its meta.json.
func readEndpoint(path string) (endpoint, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return endpoint{}, fmt.Errorf("not found (no %s; see docker context ls)", path)
	}
	if err != nil {
		return endpoint{}, err
	}
	var m contextMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return endpoint{}, fmt.Errorf("reading %s: %w", path, err)
	}
	if m.Endpoints.Docker.Host == "" {
		return endpoint{}, errors.New("it has no docker endpoint")
	}
	return m.Endpoints.Docker, nil
}
