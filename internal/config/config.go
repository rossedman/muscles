// Package config resolves muscles settings with a published precedence:
// flags, then MUSCLES_* environment variables, then the project file
// (.muscles.yaml, found by searching up from the working directory), then
// the user file ($XDG_CONFIG_HOME/muscles/config.yaml), then defaults.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/viper"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/check"
	"github.com/rossedman/muscles/internal/size"
	"github.com/rossedman/muscles/internal/source"
)

// EnvPrefix prefixes every environment variable.
const EnvPrefix = "MUSCLES"

// ProjectFiles are the project config names, in lookup order.
var ProjectFiles = []string{".muscles.yaml", ".muscles.yml"}

// Config is the resolved configuration.
type Config struct {
	Source   string `mapstructure:"source"`
	Platform string `mapstructure:"platform"`
	Output   struct {
		Units string `mapstructure:"units"`
	} `mapstructure:"output"`
	Cache struct {
		Dir     string `mapstructure:"dir"`
		MaxSize string `mapstructure:"max_size"`
	} `mapstructure:"cache"`
	Check struct {
		MaxSize       string   `mapstructure:"max_size"`
		MaxCompressed string   `mapstructure:"max_compressed"`
		MaxWaste      string   `mapstructure:"max_waste"`
		MaxGrowth     string   `mapstructure:"max_growth"`
		MaxLayers     int      `mapstructure:"max_layers"`
		Forbid        []string `mapstructure:"forbid"`
		Allow         []string `mapstructure:"allow"`
	} `mapstructure:"check"`
	Warn struct {
		LargeFile         string `mapstructure:"large_file"`
		PermissionRewrite string `mapstructure:"permission_rewrite"`
		PackageCache      bool   `mapstructure:"package_cache"`
	} `mapstructure:"warn"`
}

// Key describes one setting.
type Key struct {
	Name    string
	Default any
	Help    string
}

// Keys lists every setting, in documentation order.
var Keys = []Key{
	{"source", "auto", "where images come from: auto, docker, remote, tar, oci"},
	{"platform", "", "platform of multi-platform images, like linux/arm64 (default: linux on this machine's architecture)"},
	{"output.units", "si", "size units: si (MB) or iec (MiB)"},
	{"cache.dir", "", "layer index cache (default: $XDG_CACHE_HOME/muscles)"},
	{"cache.max_size", "2GB", "cache size cap; least recently used layers go first"},
	{"check.max_size", "", "fail when the uncompressed image is bigger"},
	{"check.max_compressed", "", "fail when the compressed image (what you pull) is bigger"},
	{"check.max_waste", "", "fail when waste is bigger: bytes, or a percentage of the image"},
	{"check.max_growth", "", "fail when the image grew more than this vs --baseline: bytes or a percentage"},
	{"check.max_layers", 0, "fail when the image has more layers"},
	{"check.forbid", []string{}, "glob patterns that must not exist in the final image; a trailing / matches directories"},
	{"check.allow", []string{}, "exceptions to check.forbid"},
	{"warn.large_file", "50MB", "note single files at least this big (0 turns it off)"},
	{"warn.permission_rewrite", "1MB", "note files stored again only for a permission or owner change (0 turns it off)"},
	{"warn.package_cache", true, "note package manager caches left in a layer"},
}

// EnvVar is the environment variable for a key: check.max_size is
// MUSCLES_CHECK_MAX_SIZE.
func EnvVar(key string) string {
	return EnvPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// Files are the config files that were read.
type Files struct {
	User    string
	Project string
	user    *viper.Viper
	project *viper.Viper
}

// Origin names the layer a key's value came from, other than flags (which
// the CLI knows about): "env MUSCLES_X", "project PATH", "user PATH" or
// "default".
func (f Files) Origin(key string) string {
	switch {
	case os.Getenv(EnvVar(key)) != "":
		return "env " + EnvVar(key)
	case f.project != nil && f.project.InConfig(key):
		return "project " + f.Project
	case f.user != nil && f.user.InConfig(key):
		return "user " + f.User
	}
	return "default"
}

// Setup installs defaults and environment bindings on v.
func Setup(v *viper.Viper) {
	for _, k := range Keys {
		v.SetDefault(k.Name, k.Default)
		_ = v.BindEnv(k.Name, EnvVar(k.Name))
	}
}

// Dirs are where muscles looks for files; tests replace them.
type Dirs struct {
	Cwd       string
	ConfigDir string // user config directory, holding muscles/config.yaml
	CacheDir  string
}

// DefaultDirs honors XDG_CONFIG_HOME and XDG_CACHE_HOME on every platform,
// then falls back to the platform's directories.
func DefaultDirs() Dirs {
	d := Dirs{}
	d.Cwd, _ = os.Getwd()
	if d.ConfigDir = os.Getenv("XDG_CONFIG_HOME"); d.ConfigDir == "" {
		d.ConfigDir, _ = os.UserConfigDir()
	}
	if d.CacheDir = os.Getenv("XDG_CACHE_HOME"); d.CacheDir == "" {
		d.CacheDir, _ = os.UserCacheDir()
	}
	return d
}

// UserFile is the user config file's path.
func (d Dirs) UserFile() string { return filepath.Join(d.ConfigDir, "muscles", "config.yaml") }

// FindProject returns the nearest project file at or above dir.
func FindProject(dir string) string {
	for dir != "" {
		for _, name := range ProjectFiles {
			p := filepath.Join(dir, name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

// Load reads the user and project files into v (explicit replaces project
// discovery) and returns the resolved configuration.
func Load(v *viper.Viper, explicit string, d Dirs) (Config, Files, error) {
	var f Files
	var err error
	if _, statErr := os.Stat(d.UserFile()); statErr == nil {
		f.User = d.UserFile()
		if f.user, err = read(f.User); err != nil {
			return Config{}, f, err
		}
	}
	f.Project = explicit
	if explicit == "" {
		f.Project = FindProject(d.Cwd)
	} else if _, statErr := os.Stat(explicit); statErr != nil {
		return Config{}, f, fmt.Errorf("config file %s: %w", explicit, errors.Unwrap(statErr))
	}
	if f.Project != "" {
		if f.project, err = read(f.Project); err != nil {
			return Config{}, f, err
		}
	}
	for _, sub := range []*viper.Viper{f.user, f.project} {
		if sub != nil {
			if err := v.MergeConfigMap(sub.AllSettings()); err != nil {
				return Config{}, f, err
			}
		}
	}
	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return Config{}, f, fmt.Errorf("config: %w", err)
	}
	if c.Cache.Dir == "" && d.CacheDir != "" {
		c.Cache.Dir = filepath.Join(d.CacheDir, "muscles")
	}
	return c, f, c.Validate()
}

// read loads one YAML file and rejects keys muscles doesn't know.
func read(path string) (*viper.Viper, error) {
	sub := viper.New()
	sub.SetConfigFile(path)
	sub.SetConfigType("yaml")
	if err := sub.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	known := map[string]bool{}
	var names []string
	for _, k := range Keys {
		known[k.Name] = true
		names = append(names, k.Name)
	}
	keys := sub.AllKeys()
	sort.Strings(keys)
	for _, k := range keys {
		if !known[k] {
			return nil, fmt.Errorf("unknown setting %q in %s%s", k, path, suggest(k, names))
		}
	}
	return sub, nil
}

func suggest(k string, names []string) string {
	best, bestD := "", 4
	for _, n := range names {
		if d := Distance(k, n); d < bestD {
			best, bestD = n, d
		}
	}
	if best == "" {
		return " (run `muscles config list` to see every setting)"
	}
	return fmt.Sprintf(" (did you mean %q?)", best)
}

// Distance is the Levenshtein edit distance, for did-you-mean hints.
func Distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// Validate checks every value parses.
func (c Config) Validate() error {
	if _, err := source.ParseKind(c.Source); err != nil {
		return err
	}
	if _, err := source.ParsePlatform(c.Platform); err != nil {
		return err
	}
	if c.Output.Units != "si" && c.Output.Units != "iec" {
		return fmt.Errorf("invalid output.units %q (use si or iec)", c.Output.Units)
	}
	if _, err := c.CacheMax(); err != nil {
		return err
	}
	if _, err := c.Budget(); err != nil {
		return err
	}
	_, err := c.Analysis()
	return err
}

// IEC reports whether sizes print in powers of 1024.
func (c Config) IEC() bool { return c.Output.Units == "iec" }

// CacheMax is the cache size cap in bytes.
func (c Config) CacheMax() (int64, error) { return parse("cache.max_size", c.Cache.MaxSize) }

func parse(key, s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	n, err := size.ParseBytes(s)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func parseLimit(key, s string) (size.Limit, error) {
	l, err := size.ParseLimit(s)
	if err != nil {
		return l, fmt.Errorf("%s: %w", key, err)
	}
	return l, nil
}

// Budget is the check configuration.
func (c Config) Budget() (check.Budget, error) {
	var b check.Budget
	var err error
	if b.MaxSize, err = parse("check.max_size", c.Check.MaxSize); err != nil {
		return b, err
	}
	if b.MaxCompressed, err = parse("check.max_compressed", c.Check.MaxCompressed); err != nil {
		return b, err
	}
	if b.MaxWaste, err = parseLimit("check.max_waste", c.Check.MaxWaste); err != nil {
		return b, err
	}
	if b.MaxGrowth, err = parseLimit("check.max_growth", c.Check.MaxGrowth); err != nil {
		return b, err
	}
	if c.Check.MaxLayers < 0 {
		return b, fmt.Errorf("check.max_layers: must be 0 or more, got %d", c.Check.MaxLayers)
	}
	b.MaxLayers, b.Forbid, b.Allow = c.Check.MaxLayers, c.Check.Forbid, c.Check.Allow
	return b, b.Validate()
}

// Analysis is the note configuration.
func (c Config) Analysis() (analysis.Options, error) {
	o := analysis.Options{PackageCache: c.Warn.PackageCache}
	var err error
	if o.LargeFile, err = parse("warn.large_file", c.Warn.LargeFile); err != nil {
		return o, err
	}
	o.PermissionRewrite, err = parse("warn.permission_rewrite", c.Warn.PermissionRewrite)
	return o, err
}
