package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/rossedman/muscles/internal/testimg"
)

// appDockerfile is a Dockerfile the test app could have been built from.
const appDockerfile = `FROM python:3.12-slim
ENV PYTHONUNBUFFERED=1
RUN apt-get update && apt-get install -y curl
WORKDIR /app
RUN pip install -r requirements.txt && rm -rf /var/lib/apt/lists/*
COPY . .
RUN chown -R 1000:1000 /app
CMD ["python", "main.py"]
`

func (e *env) dockerfile() string {
	p := filepath.Join(e.dirs.Cwd, "Dockerfile")
	if err := os.WriteFile(p, []byte(appDockerfile), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func TestImagesInspect(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	r := e.run("images", "inspect", fixtures.v1)
	must(t, r, 0, []string{"251 KB · 1.8 KB to pull · 89 KB wasted (36%) · 6 layers", "FIX THESE FIRST (saves 89 KB)",
		" 1  saves 60 KB  L5 RUN chown -R 1000:1000 /app", "→ Set the owner", "Leaves the apt lists", "BIGGEST LAYERS", "mostly /usr/local/lib/python3.12/site-packages"},
		[]string{"Next: muscles changes list " + fixtures.v1 + " --layer 5   (the files behind fix 1)"})

	// The bare image runs inspect; --limit summarizes the rest.
	r = e.run(fixtures.v1, "--limit", "1", "--dockerfile", e.dockerfile())
	must(t, r, 0, []string{"L5 RUN chown -R 1000:1000 /app (Dockerfile:7)", "2 smaller fixes save 29 KB more; --limit 0 shows them."}, nil)
	if strings.Contains(r.out, "Leaves the apt lists") {
		t.Errorf("--limit 1 shows more:\n%s", r.out)
	}
	must(t, e.run(fixtures.v1, "--limit", "2"), 0, []string{"1 smaller fix saves 9 KB more; --limit 0 shows it."}, nil)
	must(t, e.run(fixtures.empty), 0, []string{"✓ No waste worth fixing."}, []string{"Next: muscles layers list"})
	must(t, e.run(testimgTar(t, e, 1)), 0, nil, nil)

	big := filepath.Join(e.dirs.Cwd, "big.tar")
	tag, _ := name.NewTag("big:1")
	img := testimg.Build(testimg.Layer("COPY", testimg.File{Path: "data.bin", Body: strings.Repeat("x", 2000)}))
	if err := tarball.WriteToFile(big, tag, img); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUSCLES_WARN_LARGE_FILE", "1KB")
	must(t, e.run(big), 0, []string{"WORTH A LOOK", "A single 2 KB file: /data.bin."}, nil)
	must(t, e.run(big, "-o", "markdown"), 0, []string{"**Worth a look**", "- `L0 COPY`: A single 2 KB file: /data.bin."}, nil)
	t.Setenv("MUSCLES_WARN_LARGE_FILE", "")

	e.tty = false
	r = e.run(fixtures.v1, "--dockerfile", e.dockerfile())
	must(t, r, 0, []string{"1\t60208\t5\t7\tRUN chown -R 1000:1000 /app\tStores 3 files", "3\t9000\t1\t3\t"}, nil)
	if r.err != "" {
		t.Errorf("hint off a terminal: %q", r.err)
	}
	m := decode(t, e.run("images", "inspect", fixtures.v1, "--json", "--dockerfile", e.dockerfile()).out)
	fixes := m["fixes"].([]any)
	if m["saves"].(float64) != 89208 || len(fixes) != 3 || fixes[0].(map[string]any)["dockerfile_line"].(float64) != 7 || len(m["biggest_layers"].([]any)) != 3 {
		t.Errorf("json: %v", m)
	}
	must(t, e.run(fixtures.v1, "-o", "markdown", "--limit", "1"), 0, []string{"### " + fixtures.v1, "**Fix these first** (saves 89 KB)", "1. **Saves 60 KB** in `L5 RUN chown -R 1000:1000 /app`:", "2 smaller fixes", "**Biggest layers**"}, nil)
	must(t, e.run(fixtures.empty, "-o", "markdown"), 0, []string{"No waste worth fixing."}, nil)
	must(t, e.run(fixtures.v1, "--dockerfile", filepath.Join(e.dirs.Cwd, "nope")), 2, nil, []string{"read Dockerfile", "--dockerfile PATH"})
}

func TestShortcuts(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	must(t, e.run("layers", fixtures.v1), 0, []string{"RUN chown -R 1000:1000 /app"}, []string{"Next: muscles images inspect"})
	must(t, e.run("--no-color", "waste", fixtures.v1), 0, []string{"permission-only rewrite"}, nil)
	must(t, e.run("changes", "--layer", "5", fixtures.v1), 0, []string{"/app/models"}, nil)
	must(t, e.run("layers", "lst"), 2, nil, []string{`Did you mean "list"?`})
	must(t, e.run("lyers"), 2, nil, []string{`Did you mean "layers"?`})
	must(t, e.run("layers"), 0, []string{"list"}, nil)
	must(t, e.run("layers", "--", fixtures.v1), 2, nil, nil)
	must(t, e.run("-o", "plain", "layers", "-q", fixtures.v1), 0, []string{"RUN chown"}, nil)
	if r := e.run("layers", "-q", fixtures.v1); strings.Contains(r.err, "Next:") {
		t.Errorf("--quiet printed a hint: %s", r.err)
	}
	if got := expandShortcuts(newRootCmd(newApp(Options{}, nil)), []string{"__complete", "x"}); len(got) != 2 {
		t.Errorf("completion rewritten: %v", got)
	}

	// With nothing to fix, layers points at the biggest layer.
	must(t, e.run("layers", testimgTar(t, e, 2)), 0, nil, []string{"Next: muscles changes list"})
}

func TestFixesInChecksAndDiffs(t *testing.T) {
	e := newEnv(t)
	e.tty = true
	df := e.dockerfile()
	r := e.run("images", "check", fixtures.v1, "--max-size", "1KB", "--dockerfile", df)
	must(t, r, 5, []string{"HOW TO FIX", "✗ size: Run `muscles images inspect", "⚠ package cache (Dockerfile:3): End this RUN with"}, []string{"Next: muscles images inspect"})
	must(t, e.run("images", "check", fixtures.v1, "--max-size", "1KB"), 5, []string{"⚠ package cache (L1):"}, nil)
	must(t, e.run("images", "check", fixtures.v1, "--max-size", "1KB", "-o", "markdown"), 5, []string{"**How to fix**", "- **package cache** (L1): End this RUN"}, nil)
	must(t, e.run("images", "check", fixtures.v1, "--max-size", "1GB", "-o", "markdown"), 0, []string{"**How to fix**"}, nil)

	e.tty = false
	t.Setenv("GITHUB_ACTIONS", "true")
	r = e.run("images", "check", fixtures.v1, "--max-size", "1KB", "--dockerfile", df)
	must(t, r, 5, []string{"::warning file=" + ghProperty(df) + ",line=3,title=muscles package cache::apt lists left behind: 9 KB (L1 RUN apt-get update && apt-get install -y curl)%0AFix: End this RUN", "warn\tpackage cache\t", "\tEnd this RUN with `&& rm -rf /var/lib/apt/lists/*`.\t3"}, nil)
	t.Setenv("GITHUB_ACTIONS", "")
	m := decode(t, e.run("images", "check", fixtures.v1, "--max-size", "1KB", "--json", "--dockerfile", df).out)
	res := m["results"].([]any)[1].(map[string]any)
	if res["fix"] == nil || res["layer"].(float64) != 1 || res["dockerfile_line"].(float64) != 3 {
		t.Errorf("check json: %v", res)
	}

	e.tty = true
	must(t, e.run("images", "diff", fixtures.v1, fixtures.v2, "--dockerfile", df), 0,
		[]string{"app-2.tar is 12 KB smaller than", "(-4.8%), mostly in RUN pip install", "Biggest changes: - /usr/local/lib/python3.12/site-packages/numpy/core.so", "RUN chown -R 1000:1000 /app (Dockerfile:7)"},
		[]string{"Next: muscles changes list " + fixtures.v2 + " --layer 3"})
	m = decode(t, e.run("images", "diff", fixtures.v1, fixtures.v2, "--json").out)
	if !strings.Contains(m["summary"].(string), "smaller than") || len(m["packages"].(map[string]any)["installed"].([]any)) != 0 {
		t.Errorf("diff json: %v", m)
	}

	pkgs := func(file, list string) string {
		var b strings.Builder
		for _, p := range strings.Fields(list) {
			n, v, _ := strings.Cut(p, "=")
			b.WriteString("Package: " + n + "\nStatus: install ok installed\nVersion: " + v + "\n\n")
		}
		p := filepath.Join(e.dirs.Cwd, file)
		tag, _ := name.NewTag("pkgs:1")
		if err := tarball.WriteToFile(p, tag, testimg.Build(testimg.Layer("RUN apt-get install", testimg.File{Path: "var/lib/dpkg/status", Body: b.String()}))); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, b := pkgs("a.tar", "curl=1 libc6=2.36 wget=1"), pkgs("b.tar", "curl=1 libc6=2.37 git=2.39")
	must(t, e.run("images", "diff", a, b), 0, []string{"Packages: installed git 2.39 (dpkg); upgraded libc6 2.36 → 2.37; removed wget 1 (dpkg).", "PACKAGES (3 changes)", "git", "2.36 → 2.37", "wget"}, nil)
	must(t, e.run("images", "diff", a, b, "-o", "markdown"), 0, []string{"**Packages** (3 changes)", "| + | git | 2.39 | dpkg |"}, nil)
	must(t, e.run("layers", b), 0, []string{"installed curl 1, git 2.39 and libc6 2.37 (dpkg)"}, nil)
}
