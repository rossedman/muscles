package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func dirs(t *testing.T) Dirs {
	t.Helper()
	root := t.TempDir()
	d := Dirs{Cwd: filepath.Join(root, "repo", "svc"), ConfigDir: filepath.Join(root, "config"), CacheDir: filepath.Join(root, "cache")}
	os.MkdirAll(d.Cwd, 0o755)
	os.MkdirAll(filepath.Join(d.ConfigDir, "muscles"), 0o755)
	return d
}

func load(t *testing.T, d Dirs, explicit string) (Config, Files, error) {
	t.Helper()
	v := viper.New()
	Setup(v)
	return Load(v, explicit, d)
}

func TestDefaults(t *testing.T) {
	d := dirs(t)
	c, f, err := load(t, d, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != "auto" || c.Output.Units != "si" || c.Cache.MaxSize != "2GB" || !c.Warn.PackageCache || c.Cache.Dir != filepath.Join(d.CacheDir, "muscles") {
		t.Fatalf("defaults: %+v", c)
	}
	if f.User != "" || f.Project != "" || f.Origin("source") != "default" {
		t.Fatalf("files: %+v", f)
	}
	o, _ := c.Analysis()
	if o.LargeFile != 50_000_000 || o.PermissionRewrite != 1_000_000 {
		t.Fatalf("analysis: %+v", o)
	}
	if c.IEC() {
		t.Fatal("iec")
	}
}

func TestPrecedence(t *testing.T) {
	d := dirs(t)
	os.WriteFile(d.UserFile(), []byte("output:\n  units: iec\ncheck:\n  max_size: 1GB\n  max_layers: 9\n"), 0o644)
	project := filepath.Join(filepath.Dir(d.Cwd), ".muscles.yaml")
	os.WriteFile(project, []byte("check:\n  max_size: 800MB\n  max_waste: 10%\n  forbid: ['**/.env']\n"), 0o644)
	t.Setenv("MUSCLES_CHECK_MAX_LAYERS", "40")

	c, f, err := load(t, d, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Output.Units != "iec" || c.Check.MaxSize != "800MB" || c.Check.MaxLayers != 40 || len(c.Check.Forbid) != 1 {
		t.Fatalf("merged: %+v", c)
	}
	if f.Project != project || f.User != d.UserFile() {
		t.Fatalf("files: %+v", f)
	}
	for key, want := range map[string]string{
		"output.units":     "user " + d.UserFile(),
		"check.max_size":   "project " + project,
		"check.max_layers": "env MUSCLES_CHECK_MAX_LAYERS",
		"source":           "default",
	} {
		if got := f.Origin(key); got != want {
			t.Errorf("Origin(%s) = %q, want %q", key, got, want)
		}
	}
	b, err := c.Budget()
	if err != nil || b.MaxSize != 800_000_000 || b.MaxWaste.Percent != 10 || b.MaxLayers != 40 {
		t.Fatalf("budget: %+v %v", b, err)
	}

	// An explicit file replaces the project file.
	other := filepath.Join(d.Cwd, "ci.yaml")
	os.WriteFile(other, []byte("check:\n  max_size: 5MB\n"), 0o644)
	c, f, err = load(t, d, other)
	if err != nil || c.Check.MaxSize != "5MB" || f.Project != other {
		t.Fatalf("explicit: %+v %+v %v", c, f, err)
	}
	if _, _, err := load(t, d, filepath.Join(d.Cwd, "missing.yaml")); err == nil || !strings.Contains(err.Error(), "missing.yaml") {
		t.Fatalf("missing explicit: %v", err)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]string{
		"check:\n  max_sise: 1GB\n":         `unknown setting "check.max_sise"`,
		"colour: red\n":                     "run `muscles config list`",
		"check: [\n":                        "read config",
		"source: podman\n":                  "invalid source",
		"platform: linux\n":                 "invalid platform",
		"output:\n  units: bytes\n":         "invalid output.units",
		"cache:\n  max_size: lots\n":        "cache.max_size",
		"check:\n  max_size: big\n":         "check.max_size",
		"check:\n  max_compressed: big\n":   "check.max_compressed",
		"check:\n  max_waste: x%\n":         "check.max_waste",
		"check:\n  max_growth: y\n":         "check.max_growth",
		"check:\n  max_layers: -1\n":        "check.max_layers",
		"check:\n  forbid: ['[x']\n":        "invalid path pattern",
		"warn:\n  large_file: huge\n":       "warn.large_file",
		"warn:\n  permission_rewrite: 1x\n": "warn.permission_rewrite",
		"check:\n  max_layers: [1, 2]\n":    "config:",
	}
	for body, want := range cases {
		d := dirs(t)
		os.WriteFile(filepath.Join(d.Cwd, ".muscles.yml"), []byte(body), 0o644)
		if _, _, err := load(t, d, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", body, err, want)
		}
	}
	d := dirs(t)
	os.WriteFile(d.UserFile(), []byte("nope: 1\n"), 0o644)
	if _, _, err := load(t, d, ""); err == nil {
		t.Error("bad user file accepted")
	}
}

func TestHelpers(t *testing.T) {
	if EnvVar("check.max_size") != "MUSCLES_CHECK_MAX_SIZE" {
		t.Fatal(EnvVar("check.max_size"))
	}
	if Distance("kitten", "sitting") != 3 {
		t.Fatal("distance")
	}
	if FindProject("") != "" || FindProject(t.TempDir()) != "" {
		t.Fatal("FindProject found something")
	}
	t.Setenv("XDG_CONFIG_HOME", "/x/config")
	t.Setenv("XDG_CACHE_HOME", "/x/cache")
	if d := DefaultDirs(); d.ConfigDir != "/x/config" || d.CacheDir != "/x/cache" || d.Cwd == "" {
		t.Fatalf("DefaultDirs: %+v", d)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	if d := DefaultDirs(); d.ConfigDir == "" || d.CacheDir == "" {
		t.Fatalf("DefaultDirs fallback: %+v", d)
	}
}
