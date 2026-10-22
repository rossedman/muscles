// Package cli is muscles' command-line interface: a cobra command tree,
// one command per file, over viper-resolved configuration. Commands parse
// input, call the analysis packages and render the result; they hold no
// analysis logic of their own.
//
// Streams are typed: stdout carries only data (tables, lines, JSON,
// Markdown, requested help), stderr carries progress, hints, warnings and
// errors.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/config"
	"github.com/rossedman/muscles/internal/dockerfile"
	"github.com/rossedman/muscles/internal/index"
	"github.com/rossedman/muscles/internal/size"
	"github.com/rossedman/muscles/internal/source"
	"github.com/rossedman/muscles/internal/ui"
)

// Schema names the JSON output format. Fields are only ever added within
// a schema version.
const Schema = "muscles/v1"

// Links printed at the end of help.
const (
	docsURL   = "https://rossedman.io/muscles/"
	issuesURL = "https://github.com/rossedman/muscles/issues"
)

// Options inject I/O and dependencies; zero values use the real ones.
type Options struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Terminal reports whether a stream is a terminal (tests force it).
	Terminal func(any) bool
	Dirs     *config.Dirs
	// Daemon opens images from the Docker daemon (tests fake it).
	Daemon   func(name.Reference, ...daemon.Option) (v1.Image, error)
	Keychain authn.Keychain
}

// withDefaults fills in the standard streams for any that are nil.
func (o Options) withDefaults() Options {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Terminal == nil {
		o.Terminal = ui.IsTerminal
	}
	if o.Dirs == nil {
		d := config.DefaultDirs()
		o.Dirs = &d
	}
	return o
}

// globals are the persistent flags every command accepts.
type globals struct {
	configFile string
	output     string
	json       bool
	noColor    bool
	quiet      bool
	verbose    bool
	fresh      bool
	timeout    time.Duration
}

// app is the state of one invocation.
type app struct {
	opts   Options
	v      *viper.Viper
	g      globals
	cfg    config.Config
	files  config.Files
	argv   []string
	out    io.Writer // stdout, color-aware
	errw   io.Writer // stderr, color-aware
	th     *ui.Theme // for stdout
	eth    *ui.Theme // for stderr
	status *status
	hints  []string
	cancel context.CancelFunc
	// everyFix notes waste of any size, not only above the warn thresholds,
	// so inspect can account for all of it.
	everyFix bool
}

// newApp builds the shared state for one run: options with defaults, a Viper instance with the settings registered, and the output streams.
func newApp(opts Options, argv []string) *app {
	opts = opts.withDefaults()
	v := viper.New()
	config.Setup(v)
	a := &app{opts: opts, v: v, argv: argv}
	a.setStreams(noColorArg(argv))
	return a
}

// setStreams decides color per stream: each is checked on its own, so a
// piped stdout stays plain while stderr still shows progress.
func (a *app) setStreams(noColor bool) {
	outColor := a.opts.Terminal(a.opts.Stdout) && ui.ColorAllowed(noColor)
	errColor := a.opts.Terminal(a.opts.Stderr) && ui.ColorAllowed(noColor)
	a.out, a.th = ui.NewWriter(a.opts.Stdout, outColor), ui.NewTheme(outColor)
	a.errw, a.eth = ui.NewWriter(a.opts.Stderr, errColor), ui.NewTheme(errColor)
	a.status = &status{w: a.errw, th: a.eth, live: a.opts.Terminal(a.opts.Stderr)}
}

// noColorArg finds --no-color before flags are parsed, so help and usage
// errors honor it too.
func noColorArg(argv []string) bool {
	for _, s := range argv {
		if s == "--" {
			return false
		}
		if s == "--no-color" || s == "--no-color=true" {
			return true
		}
	}
	return false
}

// viperKey is the flag annotation naming the config key a flag sets.
const viperKey = "muscles/viper-key"

// bindFlag marks a flag as setting a config key, so the flag beats the
// environment, which beats the config files, which beat the defaults.
func bindFlag(fs *pflag.FlagSet, flagName, key string) {
	_ = fs.SetAnnotation(flagName, viperKey, []string{key})
}

// load binds the running command's flags to their config keys and resolves
// the configuration.
func (a *app) load(cmd *cobra.Command) error {
	bind := func(f *pflag.Flag) {
		if keys := f.Annotations[viperKey]; len(keys) == 1 {
			_ = a.v.BindPFlag(keys[0], f)
		}
	}
	cmd.Flags().VisitAll(bind)
	cmd.InheritedFlags().VisitAll(bind)
	cfg, files, err := config.Load(a.v, a.g.configFile, *a.opts.Dirs)
	if err != nil {
		return &exitError{code: ExitUsage, kind: "CONFIG_INVALID", err: err, run: "muscles config list"}
	}
	a.cfg, a.files = cfg, files
	return nil
}

// origin names where a key's value came from, flags included.
func (a *app) origin(cmd *cobra.Command, key string) string {
	found := ""
	visit := func(f *pflag.Flag) {
		if keys := f.Annotations[viperKey]; len(keys) == 1 && keys[0] == key && f.Changed {
			found = "flag --" + f.Name
		}
	}
	cmd.Flags().VisitAll(visit)
	cmd.InheritedFlags().VisitAll(visit)
	if found != "" {
		return found
	}
	return a.files.Origin(key)
}

// Output formats.
const (
	formatTable    = "table"
	formatPlain    = "plain"
	formatJSON     = "json"
	formatMarkdown = "markdown"
	formatSARIF    = "sarif" // images check only
)

// format is the output format: --json beats --output; by default a table
// on a terminal and plain lines otherwise.
func (a *app) format() (string, error) {
	switch {
	case a.g.json:
		return formatJSON, nil
	case a.g.output == "":
		if a.opts.Terminal(a.opts.Stdout) {
			return formatTable, nil
		}
		return formatPlain, nil
	}
	for _, f := range []string{formatTable, formatPlain, formatJSON, formatMarkdown, formatSARIF} {
		if a.g.output == f {
			return f, nil
		}
	}
	return "", usageErrorf("invalid --output %q (use table, plain, json, markdown or sarif)", a.g.output)
}

// jsonOutput reports whether the chosen output format is JSON.
func (a *app) jsonOutput() bool {
	f, _ := a.format()
	return f == formatJSON
}

// printJSON writes v to stdout as indented JSON.
func (a *app) printJSON(v any) error { return jsonEncoder(a.opts.Stdout).Encode(v) }

// render writes t in format f. Markdown output gets title as a heading.
func (a *app) render(f string, t *ui.Table, title string) error {
	switch f {
	case formatPlain:
		return t.Plain(a.opts.Stdout)
	case formatMarkdown:
		if title != "" {
			fmt.Fprintf(a.opts.Stdout, "### %s\n\n", title)
		}
		return t.Markdown(a.opts.Stdout)
	}
	return t.Render(a.out, a.th)
}

// size formats bytes in the configured units.
func (a *app) size(n int64) string { return size.Format(n, a.cfg.IEC()) }

// delta formats a signed size change using the configured units.
func (a *app) delta(n int64) string { return size.FormatDelta(n, a.cfg.IEC()) }

// debugf prints detail on stderr only with --verbose.
func (a *app) debugf(format string, args ...any) {
	if !a.g.verbose {
		return
	}
	a.status.clear()
	fmt.Fprintln(a.errw, a.eth.Paint(a.eth.Muted, fmt.Sprintf(format, args...)))
}

// quiet is --quiet unless --verbose, which wins.
func (a *app) quiet() bool { return a.g.quiet && !a.g.verbose }

// hint queues a line printed on stderr after the command succeeds.
func (a *app) hint(format string, args ...any) {
	a.hints = append(a.hints, fmt.Sprintf(format, args...))
}

// next suggests the command to run next. Only a person at a terminal sees
// it: in logs and pipelines it would be noise.
func (a *app) next(command, why string) {
	if !a.opts.Terminal(a.opts.Stderr) {
		return
	}
	line := "Next: " + command
	if why != "" {
		line += "   (" + why + ")"
	}
	a.hints = append(a.hints, line)
}

// wrap word-wraps text for the terminal: the first line after first, the
// rest after indent, both of which are already on the line. Lines stay
// within 100 columns, so prose reads well on wide terminals too.
func (a *app) wrap(text string, first, indent int) string {
	width := ui.Width(a.opts.Stdout)
	if width <= 0 || width > 100 {
		width = 100
	}
	lines := strings.Split(ansi.Wordwrap(text, max(width-first, 20), ""), "\n")
	if len(lines) > 1 {
		rest := ansi.Wordwrap(strings.Join(lines[1:], " "), max(width-indent, 20), "")
		lines = append(lines[:1], strings.Split(rest, "\n")...)
	}
	return strings.Join(lines, "\n"+strings.Repeat(" ", indent))
}

// shellQuote quotes s for a shell when it needs it, for commands we print.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t'\"$`&|;<>()*?[]{}!#~\\") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// interactive reports whether a person can answer a prompt.
func (a *app) interactive() bool {
	return a.opts.Terminal(a.opts.Stdin) && a.opts.Terminal(a.opts.Stderr)
}

// args fills in missing positional arguments: at a terminal it asks for
// them and later prints the full command; otherwise it is a usage error
// naming what is missing.
func (a *app) args(cmd *cobra.Command, args []string, names ...string) ([]string, error) {
	if len(args) > len(names) {
		return nil, usageErrorf("%s takes %d argument(s), got %d: %s", cmd.CommandPath(), len(names), len(args), strings.Join(args, " ")).withCmd(cmd)
	}
	if len(args) == len(names) {
		return args, nil
	}
	if !a.interactive() {
		return nil, usageErrorf("missing %s: run `%s %s`", strings.Join(names[len(args):], " and "), cmd.CommandPath(), strings.Join(names, " ")).withCmd(cmd)
	}
	in := bufio.NewReader(a.opts.Stdin)
	out := append([]string{}, args...)
	for _, n := range names[len(args):] {
		fmt.Fprintf(a.errw, "%s %s: ", a.eth.Paint(a.eth.Head, "?"), strings.ToLower(n))
		line, _ := in.ReadString('\n')
		if line = strings.TrimSpace(line); line == "" {
			return nil, usageErrorf("missing %s", n).withCmd(cmd)
		}
		out = append(out, line)
	}
	a.hint("Hint: next time run `%s %s`", cmd.CommandPath(), strings.Join(out, " "))
	return out, nil
}

// sourceOptions builds image-opening options from the configuration.
func (a *app) sourceOptions() source.Options {
	kind, _ := source.ParseKind(a.cfg.Source)
	p, _ := source.ParsePlatform(a.cfg.Platform)
	return source.Options{Kind: kind, Platform: p, Daemon: a.opts.Daemon, Keychain: a.opts.Keychain}
}

// cache returns the layer cache, or nil when it has no directory.
func (a *app) cache() *index.Cache {
	if a.cfg.Cache.Dir == "" {
		return nil
	}
	max, _ := a.cfg.CacheMax()
	return &index.Cache{Dir: a.cfg.Cache.Dir, MaxBytes: max}
}

// analyze opens, indexes and analyzes one image.
func (a *app) analyze(ctx context.Context, ref string) (*analysis.Report, error) {
	a.status.start("opening " + ref)
	img, err := source.Open(ctx, ref, a.sourceOptions())
	if err != nil {
		a.status.clear()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, imageError(ref, err)
	}
	a.debugf("opened %s from %s (%s)", ref, img.Kind, img.Platform)
	ix := index.Indexer{Cache: a.cache(), Fresh: a.g.fresh, Progress: func(p index.Progress) {
		if !p.Done {
			a.status.set(fmt.Sprintf("indexing %s · layer %d/%d · %s", ref, p.Index+1, p.Total, a.size(p.Compressed)))
			return
		}
		how := "indexed"
		if p.Cached {
			how = "cached"
		}
		a.debugf("%s layer %d/%d %s (%s) in %s", how, p.Index+1, p.Total, shortDigest(p.DiffID), a.size(p.Compressed), p.Elapsed.Round(time.Millisecond))
	}}
	ii, err := ix.Index(ctx, img)
	a.status.clear()
	if err != nil {
		return nil, fmt.Errorf("index %s: %w", ref, err)
	}
	opts, _ := a.cfg.Analysis()
	if a.everyFix {
		opts.PermissionRewrite, opts.Overwrite = 1, 1
	}
	return analysis.Analyze(ii, opts), nil
}

// analyzeBuilt analyzes the image being explained (not a baseline) and,
// with a Dockerfile configured, points each layer at its line.
func (a *app) analyzeBuilt(ctx context.Context, ref string) (*analysis.Report, error) {
	r, err := a.analyze(ctx, ref)
	if err != nil || a.cfg.Dockerfile == "" {
		return r, err
	}
	f, err := os.Open(a.cfg.Dockerfile)
	if err != nil {
		return nil, &exitError{code: ExitUsage, kind: "USAGE", err: fmt.Errorf("read Dockerfile: %w", err), run: "muscles " + strings.Join(a.argv, " ") + " --dockerfile PATH"}
	}
	defer func() { _ = f.Close() }()
	ins, err := dockerfile.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("read Dockerfile %s: %w", a.cfg.Dockerfile, err)
	}
	steps := make([]string, len(r.Layers))
	for i, l := range r.Layers {
		steps[i] = l.Step
	}
	matched := 0
	for i, line := range dockerfile.Lines(steps, ins) {
		r.Layers[i].Line = line
		if line > 0 {
			matched++
		}
	}
	a.debugf("matched %d of %d layers to lines in %s", matched, len(r.Layers), a.cfg.Dockerfile)
	return r, nil
}

// stepWithLine is a layer's step, with its Dockerfile line when known.
func (a *app) stepWithLine(l analysis.Layer) string {
	step := l.Step
	if step == "" {
		step = "?"
	}
	return step + a.lineRef(l.Line)
}

// lineRef is " (Dockerfile:12)", or "" when the line is unknown.
func (a *app) lineRef(line int) string {
	if line <= 0 {
		return ""
	}
	return fmt.Sprintf(" (%s:%d)", filepath.Base(a.cfg.Dockerfile), line)
}

// stepCell shows prefix and a layer's step in a table, keeping the
// Dockerfile line when the step is cut short.
func (a *app) stepCell(prefix string, l analysis.Layer) ui.Cell {
	return ui.Cell{Text: prefix + a.stepWithLine(l), Tail: len(a.lineRef(l.Line))}
}

// shortDigest trims a digest to its algorithm and 12 hex characters.
func shortDigest(d string) string {
	if i := strings.Index(d, ":"); i >= 0 && len(d) > i+13 {
		return d[:i+13]
	}
	return d
}

// imageInfo is an image's summary in JSON output.
type imageInfo struct {
	Ref        string `json:"ref"`
	Source     string `json:"source"`
	Platform   string `json:"platform"`
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
	Compressed int64  `json:"compressed"`
	Waste      int64  `json:"waste"`
	Layers     int    `json:"layers"`
}

// infoOf summarizes a report as an imageInfo for JSON output.
func infoOf(r *analysis.Report) imageInfo {
	return imageInfo{Ref: r.Ref, Source: r.Source, Platform: r.Platform, Digest: r.Digest, Size: r.Size, Compressed: r.Compressed, Waste: r.Waste, Layers: len(r.Layers)}
}

// changeCell renders a change marker in its color, with the word for
// line-oriented output.
func (a *app) changeCell(t analysis.ChangeType) ui.Cell {
	st := map[analysis.ChangeType]func(string) string{
		analysis.Added:    a.th.Painter(a.th.Added),
		analysis.Modified: a.th.Painter(a.th.Modified),
		analysis.Meta:     a.th.Painter(a.th.Meta),
		analysis.Deleted:  a.th.Painter(a.th.Deleted),
	}[t]
	return ui.Cell{Text: t.Symbol(), Plain: string(t), Paint: st}
}

// sizeCell shows a human size, and exact bytes in plain output.
func (a *app) sizeCell(n int64) ui.Cell {
	return ui.Cell{Text: a.size(n), Plain: fmt.Sprint(n)}
}
