package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rossedman/muscles/internal/cli"
)

// fakeEnv is an env backed by a map, with home set to home.
func fakeEnv(vars map[string]string, home string) env {
	return env{
		getenv: func(k string) string { return vars[k] },
		setenv: func(k, v string) error { vars[k] = v; return nil },
		home:   func() (string, error) { return home, nil },
	}
}

// writeFile writes body to path, creating its directory.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// contextID is the directory name docker gives a context.
func contextID(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

// addContext stores a context named name in the config dir dir.
func addContext(t *testing.T, dir, name, meta string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "contexts", "meta", contextID(name), "meta.json"), meta)
}

// errWriter fails every write.
type errWriter struct{}

// Write fails.
func (errWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestMetadata(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"docker-cli-plugin-metadata"}, &out, &bytes.Buffer{}, fakeEnv(map[string]string{}, t.TempDir())); code != cli.ExitOK {
		t.Fatalf("exit %d", code)
	}
	var m map[string]string
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	for _, k := range []string{"SchemaVersion", "Vendor", "Version", "ShortDescription", "URL"} {
		if m[k] == "" {
			t.Errorf("%s is empty", k)
		}
	}
	if m["SchemaVersion"] != "0.1.0" {
		t.Errorf("SchemaVersion %q", m["SchemaVersion"])
	}
	if code := run([]string{"docker-cli-plugin-metadata"}, errWriter{}, &bytes.Buffer{}, fakeEnv(map[string]string{}, t.TempDir())); code != cli.ExitFailure {
		t.Fatalf("write failure: exit %d", code)
	}
}

func TestArgs(t *testing.T) {
	defer func(f func(context.Context, []string, cli.Options) int) { runCLI = f }(runCLI)
	var got []string
	runCLI = func(_ context.Context, argv []string, _ cli.Options) int { got = argv; return 7 }
	for _, c := range []struct{ in, want []string }{
		{[]string{"muscles", "layers", "x"}, []string{"layers", "x"}},
		{[]string{"layers", "x"}, []string{"layers", "x"}},
		{nil, nil},
		{[]string{"muscles"}, []string{}},
	} {
		got = nil
		if code := run(c.in, &bytes.Buffer{}, &bytes.Buffer{}, fakeEnv(map[string]string{}, t.TempDir())); code != 7 {
			t.Errorf("%q: exit %d, want the CLI's 7", c.in, code)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%q: ran %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRunBadContext(t *testing.T) {
	defer func(f func(context.Context, []string, cli.Options) int) { runCLI = f }(runCLI)
	runCLI = func(context.Context, []string, cli.Options) int { t.Fatal("ran the CLI"); return 0 }
	var stderr bytes.Buffer
	vars := map[string]string{"DOCKER_CONTEXT": "colima"}
	if code := run([]string{"muscles", "layers", "x"}, &bytes.Buffer{}, &stderr, fakeEnv(vars, t.TempDir())); code != cli.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(stderr.String(), "docker muscles: ") || !strings.Contains(stderr.String(), `"colima"`) {
		t.Fatalf("stderr: %q", stderr.String())
	}
}

func TestRunEndToEnd(t *testing.T) {
	vars := map[string]string{"DOCKER_HOST": "unix:///nowhere.sock"}
	if code := run([]string{"muscles", "version", "--json"}, &bytes.Buffer{}, &bytes.Buffer{}, fakeEnv(vars, t.TempDir())); code != cli.ExitOK {
		t.Fatalf("exit %d", code)
	}
}

func TestApplyContext(t *testing.T) {
	colima := `{"Name":"colima","Endpoints":{"docker":{"Host":"unix:///colima.sock"}}}`
	remote := `{"Name":"remote","Endpoints":{"docker":{"Host":"tcp://remote:2376","SkipTLSVerify":%v}}}`

	t.Run("DOCKER_HOST wins", func(t *testing.T) {
		vars := map[string]string{"DOCKER_HOST": "tcp://mine:2375", "DOCKER_CONTEXT": "bogus"}
		if err := applyContext(fakeEnv(vars, t.TempDir())); err != nil || vars["DOCKER_HOST"] != "tcp://mine:2375" {
			t.Fatalf("%v %v", err, vars)
		}
	})

	t.Run("no config.json", func(t *testing.T) {
		vars := map[string]string{}
		if err := applyContext(fakeEnv(vars, t.TempDir())); err != nil || len(vars) != 0 {
			t.Fatalf("%v %v", err, vars)
		}
	})

	t.Run("default context", func(t *testing.T) {
		home := t.TempDir()
		writeFile(t, filepath.Join(home, ".docker", "config.json"), `{"currentContext":"default"}`)
		vars := map[string]string{}
		if err := applyContext(fakeEnv(vars, home)); err != nil || len(vars) != 0 {
			t.Fatalf("%v %v", err, vars)
		}
	})

	t.Run("currentContext", func(t *testing.T) {
		home := t.TempDir()
		dir := filepath.Join(home, ".docker")
		writeFile(t, filepath.Join(dir, "config.json"), `{"currentContext":"colima"}`)
		addContext(t, dir, "colima", colima)
		vars := map[string]string{}
		if err := applyContext(fakeEnv(vars, home)); err != nil {
			t.Fatal(err)
		}
		if vars["DOCKER_HOST"] != "unix:///colima.sock" || vars["DOCKER_CERT_PATH"] != "" || vars["DOCKER_TLS_VERIFY"] != "" {
			t.Fatalf("%v", vars)
		}
	})

	t.Run("DOCKER_CONTEXT and DOCKER_CONFIG", func(t *testing.T) {
		home, dir := t.TempDir(), t.TempDir()
		writeFile(t, filepath.Join(home, ".docker", "config.json"), `{"currentContext":"bogus"}`)
		writeFile(t, filepath.Join(dir, "config.json"), `{"currentContext":"also-bogus"}`)
		addContext(t, dir, "colima", colima)
		vars := map[string]string{"DOCKER_CONTEXT": "colima", "DOCKER_CONFIG": dir}
		if err := applyContext(fakeEnv(vars, home)); err != nil || vars["DOCKER_HOST"] != "unix:///colima.sock" {
			t.Fatalf("%v %v", err, vars)
		}
	})

	for _, skip := range []bool{false, true} {
		t.Run(fmt.Sprintf("TLS skip %v", skip), func(t *testing.T) {
			dir := t.TempDir()
			addContext(t, dir, "remote", fmt.Sprintf(remote, skip))
			tls := filepath.Join(dir, "contexts", "tls", contextID("remote"), "docker")
			writeFile(t, filepath.Join(tls, "ca.pem"), "ca")
			vars := map[string]string{"DOCKER_CONTEXT": "remote", "DOCKER_CONFIG": dir}
			if err := applyContext(fakeEnv(vars, t.TempDir())); err != nil {
				t.Fatal(err)
			}
			want := "1"
			if skip {
				want = ""
			}
			if vars["DOCKER_HOST"] != "tcp://remote:2376" || vars["DOCKER_CERT_PATH"] != tls || vars["DOCKER_TLS_VERIFY"] != want {
				t.Fatalf("skip %v: %v", skip, vars)
			}
		})
	}
}

func TestApplyContextErrors(t *testing.T) {
	good := `{"Endpoints":{"docker":{"Host":"tcp://remote:2376"}}}`

	t.Run("no home", func(t *testing.T) {
		e := fakeEnv(map[string]string{}, "")
		e.home = func() (string, error) { return "", errors.New("no $HOME") }
		if err := applyContext(e); err == nil || !strings.Contains(err.Error(), "no $HOME") {
			t.Fatal(err)
		}
	})

	for _, c := range []struct {
		name  string
		setup func(t *testing.T, dir string)
		ctx   string
		want  string
	}{
		{"bad config.json", func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, "config.json"), "{") }, "", "config.json"},
		{"unreadable config.json", func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "config.json"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "", "config.json"},
		{"missing context", func(*testing.T, string) {}, "colima", `docker context "colima": not found`},
		{"unreadable meta.json", func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, "contexts", "meta", contextID("colima"), "meta.json"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, "colima", `docker context "colima"`},
		{"bad meta.json", func(t *testing.T, dir string) { addContext(t, dir, "colima", "{") }, "colima", `docker context "colima": reading`},
		{"no docker endpoint", func(t *testing.T, dir string) { addContext(t, dir, "colima", `{"Endpoints":{}}`) }, "colima", "no docker endpoint"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			c.setup(t, dir)
			err := applyContext(fakeEnv(map[string]string{"DOCKER_CONFIG": dir, "DOCKER_CONTEXT": c.ctx}, ""))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}

	// Each setenv in turn fails.
	for _, key := range []string{"DOCKER_HOST", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY"} {
		t.Run("setenv "+key, func(t *testing.T) {
			dir := t.TempDir()
			addContext(t, dir, "remote", good)
			writeFile(t, filepath.Join(dir, "contexts", "tls", contextID("remote"), "docker", "ca.pem"), "ca")
			e := fakeEnv(map[string]string{"DOCKER_CONFIG": dir, "DOCKER_CONTEXT": "remote"}, "")
			set := e.setenv
			e.setenv = func(k, v string) error {
				if k == key {
					return errors.New("setenv failed")
				}
				return set(k, v)
			}
			if err := applyContext(e); err == nil || !strings.Contains(err.Error(), `docker context "remote": setenv failed`) {
				t.Fatal(err)
			}
		})
	}
}
