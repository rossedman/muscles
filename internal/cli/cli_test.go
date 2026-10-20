package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/rossedman/muscles/internal/config"
	"github.com/rossedman/muscles/internal/testimg"
)

// fixtures are tarballs of the test app, written once.
var fixtures struct{ dir, v1, v2, empty string }

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "muscles-cli")
	if err != nil {
		panic(err)
	}
	fixtures.dir = dir
	write := func(file string, img v1.Image) string {
		p := filepath.Join(dir, file)
		tag, _ := name.NewTag("app:test")
		if err := tarball.WriteToFile(p, tag, img); err != nil {
			panic(err)
		}
		return p
	}
	fixtures.v1 = write("app-1.tar", testimg.App(100))
	fixtures.v2 = write("app-2.tar", testimg.AppNext(100))
	fixtures.empty = write("empty.tar", testimg.Build(testimg.Empty("CMD true")))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	code     int
	out, err string
}

// env is one test's isolated directories and terminal setting.
type env struct {
	t     *testing.T
	dirs  config.Dirs
	tty   bool
	stdin string
	opts  Options
}

func newEnv(t *testing.T) *env {
	t.Helper()
	root := t.TempDir()
	d := config.Dirs{Cwd: filepath.Join(root, "work"), ConfigDir: filepath.Join(root, "config"), CacheDir: filepath.Join(root, "cache")}
	os.MkdirAll(d.Cwd, 0o755)
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("GITHUB_ACTIONS", "")
	for _, k := range config.Keys {
		t.Setenv(config.EnvVar(k.Name), "")
	}
	return &env{t: t, dirs: d, opts: Options{Daemon: func(name.Reference, ...daemon.Option) (v1.Image, error) {
		return nil, errors.New("Cannot connect to the Docker daemon")
	}}}
}

func (e *env) run(args ...string) result {
	return e.runCtx(context.Background(), args...)
}

func (e *env) runCtx(ctx context.Context, args ...string) result {
	e.t.Helper()
	var out, errb bytes.Buffer
	o := e.opts
	o.Stdin, o.Stdout, o.Stderr = strings.NewReader(e.stdin), &out, &errb
	o.Dirs = &e.dirs
	tty := e.tty
	o.Terminal = func(any) bool { return tty }
	code := Run(ctx, args, o)
	return result{code: code, out: out.String(), err: errb.String()}
}

func (e *env) project(body string) {
	if err := os.WriteFile(filepath.Join(e.dirs.Cwd, ".muscles.yaml"), []byte(body), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// must checks the exit code and that each stream contains the strings,
// ignoring color.
func must(t *testing.T, r result, code int, outHas, errHas []string) {
	t.Helper()
	r.out, r.err = ansi.Strip(r.out), ansi.Strip(r.err)
	if r.code != code {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, code, r.out, r.err)
	}
	for _, s := range outHas {
		if !strings.Contains(r.out, s) {
			t.Errorf("stdout lacks %q:\n%s", s, r.out)
		}
	}
	for _, s := range errHas {
		if !strings.Contains(r.err, s) {
			t.Errorf("stderr lacks %q:\n%s", s, r.err)
		}
	}
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	return m
}

func TestLayersList(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	r := e.run("layers", "list", fixtures.v1)
	must(t, r, 0, []string{"STEP", "RUN apt-get update && apt-get install -y curl", "⚠ apt lists left behind", "ENV PYTHONUNBUFFERED=1", `CMD ["python" "main.py"]`, "total 251 KB uncompressed"}, nil)
	if !strings.Contains(r.out, "\x1b[") {
		t.Error("no color on a terminal")
	}

	e.tty = false
	r = e.run("layers", "list", fixtures.v1)
	must(t, r, 0, nil, nil)
	lines := strings.Split(strings.TrimSpace(r.out), "\n")
	if len(lines) != 6 || !strings.HasPrefix(lines[0], "0\t32200\t") || strings.Contains(r.out, "\x1b[") || strings.Contains(r.out, "ENV ") {
		t.Errorf("plain:\n%s", r.out)
	}

	r = e.run("layers", "list", fixtures.v1, "--json")
	must(t, r, 0, nil, nil)
	m := decode(t, r.out)
	if m["schema"] != Schema || m["image"].(map[string]any)["waste"].(float64) != 89208 || len(m["layers"].([]any)) != 6 {
		t.Errorf("json: %v", m)
	}

	r = e.run("layers", "list", fixtures.v1, "-o", "markdown")
	must(t, r, 0, []string{"### Layers of " + fixtures.v1, "| # | SIZE |", "total 251 KB"}, nil)

	r = e.run("layers", "list", fixtures.empty, "--json")
	must(t, r, 0, []string{`"layers": []`}, nil)
	e.tty = true
	must(t, e.run("layers", "list", fixtures.empty), 0, []string{"No layers in"}, nil)
	e.tty = false
	if r := e.run("layers", "list", fixtures.empty); r.out != "" {
		t.Errorf("empty plain: %q", r.out)
	}
}

func TestChangesList(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	must(t, e.run("changes", "list", fixtures.v1, "--layer", "3"), 0,
		[]string{"/usr/local/lib/python3.12/site-packages/pandas/core.py", "bytes still in L1"}, nil)
	must(t, e.run("changes", "list", fixtures.v1, "--layer", "1"), 0, []string{"was 20 KB (L0)"}, nil)
	must(t, e.run("changes", "list", fixtures.v1, "--layer", "5"), 0, []string{"same contents as L4"}, nil)
	must(t, e.run("changes", "list", fixtures.v1, "--layer", "3", "--limit", "2"), 0, []string{"/usr/local/lib/python3.12/site-packages/", "4 files"}, nil)
	must(t, e.run("changes", "list", fixtures.v1, "--layer", "2"), 0, []string{"No changes in layer 2 (WORKDIR /app) match."}, nil)
	must(t, e.run("changes", "list", fixtures.v1, "--type", "meta", "--path", "/app/"), 0, []string{"same contents as L4"}, nil)
	must(t, e.run("changes", "list", fixtures.v1, "--type", "added", "--path", "/usr/bin", "--files"), 0, []string{"/usr/bin/curl", "/usr/bin/bash"}, nil)

	e.tty = false
	r := e.run("changes", "list", fixtures.v1, "--min-size", "20KB", "--files")
	must(t, r, 0, []string{"0\tadded\t20000\t/usr/lib/libc.so.6\t1\t", "4\tadded\t60000\t/app/models/base.onnx"}, nil)
	if strings.Contains(r.out, "six.py") {
		t.Error("--min-size kept a small file")
	}
	r = e.run("changes", "list", fixtures.v1, "--layer", "3", "--json")
	m := decode(t, r.out)
	if m["layer"].(float64) != 3 || len(m["changes"].([]any)) != 6 {
		t.Errorf("json: %v", m)
	}
	r = e.run("changes", "list", fixtures.v1, "--layer", "2", "--json")
	must(t, r, 0, []string{`"changes": []`}, nil)
	if r := e.run("changes", "list", fixtures.v1, "--layer", "2"); r.out != "" {
		t.Errorf("empty plain: %q", r.out)
	}
	must(t, e.run("changes", "list", fixtures.v1, "-o", "markdown", "--layer", "4"), 0, []string{"### Changes in layer 4 of", "`/app/models/base.onnx`"}, nil)

	must(t, e.run("changes", "list", fixtures.v1, "--layer", "9"), 2, nil, []string{"has 6 layers (0 to 5)", "See `muscles changes list --help`"})
	must(t, e.run("changes", "list", fixtures.v1, "--layer", "-3"), 2, nil, []string{"invalid --layer"})
	must(t, e.run("changes", "list", fixtures.v1, "--type", "renamed"), 2, nil, []string{`invalid --type "renamed"`})
	must(t, e.run("changes", "list", fixtures.v1, "--min-size", "lots"), 2, nil, []string{"--min-size"})
}

func TestWasteList(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	must(t, e.run("waste", "list", fixtures.v1, "--limit", "3"), 0, []string{"/app/", "permission-only rewrite (3 files)", "L5 RUN chown", "total waste 89 KB (36% of 251 KB)"}, nil)
	must(t, e.run("waste", "list", fixtures.empty), 0, []string{"No waste in"}, nil)
	e.tty = false
	r := e.run("waste", "list", fixtures.v1, "--files")
	must(t, r, 0, []string{"60000\t/app/models/base.onnx\t1\tpermission-only rewrite\t4\t5"}, nil)
	if n := strings.Count(r.out, "\n"); n != 6 {
		t.Errorf("--files rows: %d", n)
	}
	m := decode(t, e.run("waste", "list", fixtures.v1, "--json").out)
	if m["waste"].(float64) != 89208 {
		t.Errorf("json: %v", m)
	}
	must(t, e.run("waste", "list", fixtures.v1, "-o", "markdown"), 0, []string{"### Waste in", "total waste"}, nil)
	if r := e.run("waste", "list", fixtures.empty); r.out != "" {
		t.Errorf("empty plain: %q", r.out)
	}
}

func TestImagesDiff(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	must(t, e.run("images", "diff", fixtures.v1, fixtures.v2), 0, []string{"LAYERS (3 of 6 shared)", "=  0", "≠  3", "-13 KB", "FILESYSTEM (5 changes)", "was 30 KB", "/app/api/retry.py"}, nil)
	must(t, e.run("images", "diff", fixtures.v1, fixtures.v1), 0, []string{"No differences in the final filesystem."}, nil)
	must(t, e.run("images", "diff", fixtures.v1, fixtures.empty), 0, []string{"removed"}, nil)
	must(t, e.run("images", "diff", fixtures.empty, fixtures.v1), 0, []string{"new"}, nil)

	e.tty = false
	r := e.run("images", "diff", fixtures.v1, fixtures.v2)
	must(t, r, 0, []string{"deleted\t25000\t/usr/local/lib/python3.12/site-packages/numpy/core.so\t1\t", "modified\t300\t/app/main.py\t1\twas 200 B"}, nil)
	m := decode(t, e.run("images", "diff", fixtures.v1, fixtures.v2, "--json").out)
	if m["shared_layers"].(float64) != 3 || len(m["changes"].([]any)) != 5 || m["size_delta"].(float64) >= 0 {
		t.Errorf("json: %v", m)
	}
	m = decode(t, e.run("images", "diff", fixtures.empty, fixtures.v1, "--json").out)
	if l := m["layers"].([]any)[0].(map[string]any); l["base_size"] != nil {
		t.Errorf("uneven json: %v", l)
	}
	r = e.run("images", "diff", fixtures.v1, fixtures.v2, "-o", "markdown")
	must(t, r, 0, []string{"### " + fixtures.v1 + " → " + fixtures.v2, "**Layers** (3 of 6 shared)", "**Filesystem** (5 changes)"}, nil)
	must(t, e.run("images", "diff", fixtures.v1, fixtures.v2, "-o", "markdown", "--files", "--limit", "1"), 0, nil, nil)
	must(t, e.run("images", "diff", fixtures.v1, fixtures.v1, "-o", "markdown"), 0, []string{"No differences."}, nil)
	must(t, e.run("images", "diff", fixtures.empty, testimgTar(t, e, 20), "-o", "markdown"), 0, []string{"<details>"}, nil)

	must(t, e.run("images", "diff", fixtures.v1), 2, nil, []string{"missing TARGET: run `muscles images diff BASE TARGET`"})
	must(t, e.run("images", "diff", fixtures.v1, filepath.Join(fixtures.dir, "nope.tar")), 4, nil, []string{"no tarball"})
	must(t, e.run("images", "diff", filepath.Join(fixtures.dir, "nope.tar"), fixtures.v1), 4, nil, nil)
}

// testimgTar writes an image with n files in one layer.
func testimgTar(t *testing.T, e *env, n int) string {
	var files []testimg.File
	for i := 0; i < n; i++ {
		files = append(files, testimg.File{Path: "d" + string(rune('a'+i)) + "/f", Body: strings.Repeat("x", i+1)})
	}
	p := filepath.Join(e.dirs.Cwd, "many.tar")
	tag, _ := name.NewTag("many:1")
	if err := tarball.WriteToFile(p, tag, testimg.Build(testimg.Layer("COPY", files...))); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImagesCheck(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	e.project("check:\n  max_size: 100KB\n  max_growth: 1%\n  forbid: ['**/.env']\n")
	r := e.run("images", "check", fixtures.v1, "--baseline", fixtures.v2)
	must(t, r, 5, []string{"✗ fail  size", "✗ fail  growth", `/app/.env matches "**/.env"`, "⚠ warn", "3 failed, 2 warnings"}, []string{"Error: 3 of 3 checks failed"})

	// Flags beat the project file.
	r = e.run("images", "check", fixtures.v1, "--max-size", "1GB", "--max-growth", "1GB", "--max-waste", "50%", "--max-layers", "10", "--max-compressed", "1GB", "--baseline", fixtures.v2)
	must(t, r, 5, []string{"✓ pass  size", "✓ pass  waste", "✓ pass  layers"}, nil)

	e.project("")
	must(t, e.run("images", "check", fixtures.empty), 0, []string{"No checks configured"}, nil)
	must(t, e.run("images", "check", fixtures.v1, "--max-size", "1GB"), 0, []string{"all checks passed, 2 warnings"}, nil)

	e.tty = false
	t.Setenv("GITHUB_ACTIONS", "true")
	r = e.run("images", "check", fixtures.v1, "--max-size", "1KB")
	must(t, r, 5, []string{"::error title=muscles size::251 KB > 1 KB (biggest layer", "::warning title=muscles package cache::", "fail\tsize\t"}, nil)
	r = e.run("images", "check", fixtures.v1, "--max-size", "1KB", "--json")
	if strings.Contains(r.out, "::error") {
		t.Error("annotations in JSON output")
	}
	m := decode(t, r.out)
	if m["ok"] != false || m["failed"].(float64) != 1 || m["warnings"].(float64) != 2 {
		t.Errorf("json: %v", m)
	}
	if !strings.Contains(r.err, `"code": "CHECK_FAILED"`) {
		t.Errorf("json error: %s", r.err)
	}
	t.Setenv("GITHUB_ACTIONS", "")
	if r := e.run("images", "check", fixtures.empty); r.out != "" || r.code != 0 {
		t.Errorf("empty plain: %+v", r)
	}
	must(t, e.run("images", "check", fixtures.v1, "--max-size", "1KB", "-o", "markdown"), 5, []string{"### Checks for", "| STATUS |"}, nil)
	must(t, e.run("images", "check", fixtures.v1, "--max-waste", "lots"), 2, nil, []string{"check.max_waste"})
	must(t, e.run("images", "check", fixtures.v1, "--baseline", filepath.Join(fixtures.dir, "nope.tar"), "--max-size", "1GB"), 4, nil, nil)
}

func TestCacheAndConfig(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	must(t, e.run("cache", "list"), 0, []string{"is empty"}, nil)
	must(t, e.run("layers", "list", fixtures.v1, "-v"), 0, nil, []string{"opened " + fixtures.v1 + " from tar", "indexed layer 1/6"})
	must(t, e.run("layers", "list", fixtures.v1, "-v"), 0, nil, []string{"cached layer 1/6"})
	must(t, e.run("layers", "list", fixtures.v1, "-v", "--fresh"), 0, nil, []string{"indexed layer 1/6"})
	must(t, e.run("cache", "list"), 0, []string{"sha256:", "6 layers"}, nil)
	m := decode(t, e.run("cache", "list", "--json").out)
	if len(m["layers"].([]any)) != 6 {
		t.Errorf("cache json: %v", m)
	}
	e.tty = false
	if r := e.run("cache", "list"); strings.Count(r.out, "\n") != 6 {
		t.Errorf("cache plain: %q", r.out)
	}
	must(t, e.run("cache", "delete", "--preview"), 0, []string{"would delete 6 cached layers"}, nil)
	must(t, e.run("cache", "delete", "--json"), 0, []string{`"layers": 6`}, nil)
	must(t, e.run("cache", "delete"), 0, []string{"deleted 0 cached layers"}, nil)
	if r := e.run("cache", "list"); r.out != "" {
		t.Errorf("cache after delete: %q", r.out)
	}

	// A cache dir that is a file fails to list.
	file := filepath.Join(e.dirs.Cwd, "file")
	os.WriteFile(file, nil, 0o644)
	must(t, e.run("cache", "list", "--cache-dir", file), 1, nil, []string{"read cache"})
	must(t, e.run("cache", "delete", "--cache-dir", file), 1, nil, nil)
	e.dirs.CacheDir = ""
	must(t, e.run("cache", "list"), 2, nil, []string{"no cache directory"})
	must(t, e.run("cache", "delete"), 2, nil, nil)
	// Without a cache, images still analyze.
	must(t, e.run("layers", "list", fixtures.v1), 0, nil, nil)

	e.project("check:\n  max_size: 800MB\n  forbid: [a, b]\n")
	t.Setenv("MUSCLES_OUTPUT_UNITS", "iec")
	r := e.run("config", "list", "--source", "tar")
	must(t, r, 0, []string{"check.max_size\t800MB\tproject ", "output.units\tiec\tenv MUSCLES_OUTPUT_UNITS", "source\ttar\tflag --source", "check.forbid\ta,b\tproject", "warn.package_cache\ttrue\tdefault"}, nil)
	m = decode(t, e.run("config", "list", "--json").out)
	if len(m["settings"].([]any)) != len(config.Keys) {
		t.Errorf("config json: %v", m)
	}
	e.tty = true
	must(t, e.run("config", "list"), 0, []string{"KEY", "ORIGIN"}, nil)
	must(t, e.run("layers", "list", fixtures.v1), 0, []string{"KiB"}, nil)

	e.project("check:\n  max_sise: 1\n")
	must(t, e.run("config", "list"), 2, nil, []string{`unknown setting "check.max_sise"`, `did you mean "check.max_size"`, "run: muscles config list"})
	if valueString(nil) != "" || valueString([]string{"a", "b"}) != "a,b" {
		t.Error("valueString")
	}
}

func TestHelpAndVersion(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	r := e.run()
	must(t, r, 0, []string{"███╗", Tagline(), "Explain an image:", "layers", "Exit codes:", "Learn more:"}, nil)
	r = e.run("--help")
	must(t, r, 0, []string{"███╗"}, nil)
	r = e.run("-h")
	must(t, r, 0, []string{"Explain container images, layer by layer", "Run `muscles --help` for examples"}, nil)
	if strings.Contains(r.out, "███╗") || strings.Contains(r.out, "Examples:") {
		t.Error("terse help is not terse")
	}
	must(t, e.run("layers"), 0, []string{"muscles layers <command>", "list"}, nil)
	r = e.run("layers", "list", "--help")
	must(t, r, 0, []string{"Examples:", "Global flags:", "IMAGE is a name"}, nil)
	must(t, e.run("layers", "list", "-h"), 0, []string{"Flags:"}, nil)
	must(t, e.run("help", "images", "check"), 0, []string{"check.max_size / --max-size"}, nil)
	must(t, e.run("help", "nope"), 2, nil, []string{`no help for "nope"`})

	e.tty = false
	r = e.run("help", "--json")
	m := decode(t, r.out)
	if m["name"] != "muscles" || len(m["commands"].([]any)) < 6 || len(m["global_flags"].([]any)) < 10 {
		t.Errorf("help json: %v", m)
	}
	m = decode(t, e.run("help", "images", "diff", "--json").out)
	if m["path"] != "muscles images diff" || len(m["examples"].([]any)) != 3 {
		t.Errorf("help json: %v", m)
	}
	if strings.Contains(e.run("--help").out, "\x1b[") {
		t.Error("color in piped help")
	}

	must(t, e.run("version"), 0, []string{"muscles ", "commit"}, nil)
	m = decode(t, e.run("version", "--json").out)
	if m["version"] == "" || m["platform"] == "" {
		t.Errorf("version json: %v", m)
	}
	must(t, e.run("--version"), 0, []string{"muscles "}, nil)
	must(t, e.run("man"), 0, []string{".TH \"MUSCLES\" \"1\""}, nil)
	must(t, e.run("completion", "bash"), 0, []string{"bash completion"}, nil)
	must(t, e.run("__complete", "--output", ""), 0, []string{"table", "markdown"}, nil)
	must(t, e.run("__complete", "--source", ""), 0, []string{"oci"}, nil)
	must(t, e.run("__complete", "--platform", ""), 0, []string{"linux/arm64"}, nil)
	must(t, e.run("__complete", "changes", "list", "--type", ""), 0, []string{"deleted"}, nil)
	must(t, e.run("__complete", "help", ""), 0, []string{"layers"}, nil)
	must(t, e.run("--no-color", "version"), 0, nil, nil)
}

func Tagline() string { return "explain container images, layer by layer" }

func TestUsageErrors(t *testing.T) {
	e := newEnv(t)
	must(t, e.run("lyers"), 2, nil, []string{`unknown command "lyers" for "muscles"`, `Did you mean "layers"?`})
	must(t, e.run("layers", "lst"), 2, nil, []string{`Did you mean "list"?`})
	must(t, e.run("layers", "list", "--jsno"), 2, nil, []string{"unknown flag: --jsno", "Did you mean --json?"})
	must(t, e.run("layers", "list", "--zzzzzzzz"), 2, nil, []string{"unknown flag"})
	must(t, e.run("layers", "list"), 2, nil, []string{"missing IMAGE: run `muscles layers list IMAGE`"})
	must(t, e.run("layers", "list", "a", "b"), 2, nil, []string{"takes 1 argument(s), got 2"})
	must(t, e.run("layers", "list", fixtures.v1, "-o", "yaml"), 2, nil, []string{`invalid --output "yaml"`})
	must(t, e.run("version", "extra"), 2, nil, nil)
	must(t, e.run("layers", "list", "Bad Ref!"), 2, nil, []string{"invalid image reference"})
	must(t, e.run("layers", "list", "--config", filepath.Join(e.dirs.Cwd, "nope.yaml"), fixtures.v1), 2, nil, []string{"nope.yaml"})

	// JSON errors are objects on stderr.
	r := e.run("layers", "list", filepath.Join(fixtures.dir, "nope.tar"), "--json")
	must(t, r, 4, nil, nil)
	if m := decode(t, r.err); m["error"].(map[string]any)["code"] != "NOT_FOUND" {
		t.Errorf("json error: %v", m)
	}
}

func TestPrompt(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	e.stdin = fixtures.v1 + "\n"
	r := e.run("layers", "list")
	must(t, r, 0, []string{"STEP"}, []string{"? image: ", "Hint: next time run `muscles layers list " + fixtures.v1 + "`"})
	e.stdin = fixtures.v1 + "\n" + fixtures.v2 + "\n"
	must(t, e.run("images", "diff"), 0, []string{"LAYERS"}, []string{"? base: ", "? target: "})
	e.stdin = "\n"
	must(t, e.run("layers", "list"), 2, nil, []string{"missing IMAGE"})
	e.stdin = fixtures.v1 + "\n"
	r = e.run("layers", "list", "-q")
	if strings.Contains(r.err, "Hint") {
		t.Error("hint with --quiet")
	}
}

func TestRegistryAndDaemon(t *testing.T) {
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	ref, _ := name.ParseReference(host + "/acme/app:1")
	if err := remote.Write(ref, testimg.App(10)); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	must(t, e.run("layers", "list", host+"/acme/app:1", "--platform", "linux/amd64", "-v"), 0, []string{"RUN apt-get"}, []string{"from remote"})
	must(t, e.run("layers", "list", host+"/acme/missing:1"), 4, nil, []string{"not in the local Docker daemon or its registry"})

	e.opts.Daemon = func(r name.Reference, _ ...daemon.Option) (v1.Image, error) { return testimg.App(10), nil }
	must(t, e.run("layers", "list", "app:dev", "-v"), 0, nil, []string{"from docker"})

	denied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer denied.Close()
	dhost := strings.TrimPrefix(denied.URL, "http://")
	must(t, e.run("layers", "list", "registry://"+dhost+"/acme/app:1"), 3, nil, []string{"registry authentication failed", "run: docker login " + dhost})

	// A registry that fails mid-read is a runtime error.
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			return
		}
		w.WriteHeader(http.StatusTeapot)
	}))
	defer broken.Close()
	must(t, e.run("layers", "list", "registry://"+strings.TrimPrefix(broken.URL, "http://")+"/a/b:1"), 1, nil, []string{"Error:"})
}

func TestInterruptTimeoutAndCrash(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	must(t, e.runCtx(ctx, "layers", "list", fixtures.v1), 130, nil, []string{"interrupted"})
	must(t, e.runCtx(ctx, "version"), 130, nil, nil)

	e.opts.Daemon = func(name.Reference, ...daemon.Option) (v1.Image, error) {
		time.Sleep(50 * time.Millisecond)
		return nil, context.DeadlineExceeded
	}
	must(t, e.run("layers", "list", "app:dev", "--source", "docker", "--timeout", "1ms"), 1, nil, []string{"timed out"})

	e.opts.Daemon = func(name.Reference, ...daemon.Option) (v1.Image, error) { panic("boom") }
	must(t, e.run("layers", "list", "app:dev"), 1, nil, []string{"Crash: internal panic in muscles", "This is a bug in muscles", "report: " + issuesURL + "/new"})
}

func TestProgressOnTerminal(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	e.opts.Daemon = func(name.Reference, ...daemon.Option) (v1.Image, error) {
		time.Sleep(250 * time.Millisecond) // let the spinner draw a frame
		return testimg.App(10), nil
	}
	r := e.run("layers", "list", "app:dev")
	must(t, r, 0, nil, []string{"opening app:dev"})
	if !strings.Contains(r.err, "\r\x1b[K") {
		t.Error("progress line not erased")
	}
	r = e.run("layers", "list", "app:dev", "-q")
	if strings.Contains(r.err, "opening") {
		t.Error("progress with --quiet")
	}
}

func TestHelpers(t *testing.T) {
	for ref, want := range map[string]string{"ghcr.io/acme/app:1": "ghcr.io", "app:dev": "docker.io", "localhost/x": "localhost", "registry://quay.io/a/b": "quay.io"} {
		if got := registryOf(ref); got != want {
			t.Errorf("registryOf(%s) = %s", ref, got)
		}
	}
	if shortDigest("sha256:0123456789abcdef") != "sha256:0123456789ab" || shortDigest("x") != "x" {
		t.Error("shortDigest")
	}
	if plural(1, "warning") != "1 warning" || plural(0, "warning") != "0 warnings" {
		t.Error("plural")
	}
	if ghEscape("a%b\nc\r") != "a%25b%0Ac%0D" {
		t.Error("ghEscape")
	}
	if !noColorArg([]string{"--no-color"}) || noColorArg([]string{"--", "--no-color"}) || noColorArg(nil) {
		t.Error("noColorArg")
	}
	if e := classify(errors.New("x")); e.code != ExitFailure {
		t.Error("classify")
	}
}
