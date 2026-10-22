package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/rossedman/muscles/internal/config"
	"github.com/rossedman/muscles/internal/source"
	"github.com/rossedman/muscles/internal/version"
)

// Execute runs muscles with the process's arguments and returns the exit
// code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, os.Args[1:], Options{})
}

// Run executes one invocation. It never calls os.Exit, so tests can drive it.
func Run(ctx context.Context, argv []string, opts Options) (code int) {
	a := newApp(opts, argv)
	defer func() {
		if r := recover(); r != nil {
			a.status.clear()
			v := version.Get()
			fmt.Fprintf(a.errw, "%s internal panic in muscles %s (%s, commit %s): %v\n\n%s\n", a.eth.Paint(a.eth.Err, "Crash:"), v.Version, v.Platform, v.Commit, r, debug.Stack())
			fmt.Fprintf(a.errw, "This is a bug in muscles, not something you did.\ncommand: muscles %s\nreport: %s/new\n", strings.Join(argv, " "), issuesURL)
			code = ExitFailure
		}
	}()
	root := newRootCmd(a)
	root.SetArgs(expandShortcuts(root, argv))
	err := root.ExecuteContext(ctx)
	if a.cancel != nil {
		a.cancel()
	}
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	code = ExitOK
	if err != nil {
		code = a.printError(err)
	}
	a.status.clear()
	// Hints follow success, and a failed check, where the next step matters most.
	if !a.quiet() && (code == ExitOK || code == ExitCheckFailed) {
		for _, h := range a.hints {
			fmt.Fprintln(a.errw, a.eth.Paint(a.eth.Muted, h))
		}
	}
	return code
}

// shortcuts are the noun commands that run their list verb when given an
// image: `muscles layers app:dev` is `muscles layers list app:dev`.
var shortcuts = map[string]bool{"layers": true, "changes": true, "waste": true}

// expandShortcuts rewrites the two shortcuts into the full commands before
// parsing: an image where a command belongs runs `images inspect`, and an
// image after layers, changes or waste runs that noun's list. A word close
// to a command name is left alone, so typos still get a suggestion.
func expandShortcuts(root *cobra.Command, argv []string) []string {
	i := positional(root, root.PersistentFlags(), argv, 0)
	if i < 0 {
		return argv
	}
	word := argv[i]
	if cmd, _, err := root.Find([]string{word}); err == nil && cmd != root {
		if !shortcuts[cmd.Name()] {
			return argv
		}
		list, _, _ := cmd.Find([]string{"list"})
		j := positional(cmd, list.Flags(), argv, i+1)
		if j < 0 || isCommandish(cmd, argv[j]) {
			return argv
		}
		return insert(argv, i+1, "list")
	}
	if isCommandish(root, word) || strings.HasPrefix(word, "__") {
		return argv
	}
	return insert(argv, i, "images", "inspect")
}

// isCommandish reports whether word names, or nearly names, a subcommand.
func isCommandish(cmd *cobra.Command, word string) bool {
	for _, c := range cmd.Commands() {
		if c.Name() == word || c.HasAlias(word) {
			return true
		}
	}
	return word == "help" || len(cmd.SuggestionsFor(word)) > 0
}

// positional returns the index of the first argument from start that isn't
// a flag or a flag's value, or -1. "--" ends the search.
func positional(cmd *cobra.Command, fs *pflag.FlagSet, argv []string, start int) int {
	for i := start; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--":
			return -1
		case strings.HasPrefix(arg, "--"):
			name, _, hasValue := strings.Cut(arg[2:], "=")
			if f := lookupFlag(cmd, fs, name, ""); f != nil && !hasValue && f.NoOptDefVal == "" {
				i++ // the flag's value
			}
		case strings.HasPrefix(arg, "-") && len(arg) > 1:
			short := arg[len(arg)-1:]
			if f := lookupFlag(cmd, fs, "", short); f != nil && len(arg) == 2 && f.NoOptDefVal == "" {
				i++
			}
		default:
			return i
		}
	}
	return -1
}

// lookupFlag finds a flag by long name, or by shorthand when name is empty, in fs and then the root's persistent flags.
func lookupFlag(cmd *cobra.Command, fs *pflag.FlagSet, name, short string) *pflag.Flag {
	for _, set := range []*pflag.FlagSet{fs, cmd.Root().PersistentFlags()} {
		if name != "" {
			if f := set.Lookup(name); f != nil {
				return f
			}
		} else if f := set.ShorthandLookup(short); f != nil {
			return f
		}
	}
	return nil
}

// insert returns a copy of argv with words added at index at.
func insert(argv []string, at int, words ...string) []string {
	out := append([]string{}, argv[:at]...)
	out = append(out, words...)
	return append(out, argv[at:]...)
}

const rootLong = `muscles explains container images: what each layer added, changed and
deleted, where the wasted space is, and what changed between two versions.
It reads images from the local Docker daemon, a registry (without pulling),
a docker save tarball or an OCI layout, and runs the same on a laptop and
in CI.

Nothing leaves your machine except reads from the registries you name.
Credentials come from your Docker config and credential helpers, never
from flags.`

// newRootCmd builds `muscles`: the persistent flags, help and usage wiring, and every noun command.
func newRootCmd(a *app) *cobra.Command {
	cobra.EnableCommandSorting = false // list commands in workflow order
	root := &cobra.Command{
		Use:   "muscles",
		Short: "Explain container images, layer by layer",
		Long:  rootLong,
		Example: `  # What each layer of an image did
  muscles layers list python:3.12-slim

  # The files one layer added, changed or deleted
  muscles changes list app:dev --layer 4

  # Fail a CI build that breaks the budgets in .muscles.yaml
  muscles images check app.tar --baseline ghcr.io/acme/app:main`,
		Version:                    version.Get().Version,
		Args:                       nounArgs,
		SuggestionsMinimumDistance: 2,
		RunE:                       func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
		SilenceErrors:              true,
		SilenceUsage:               true,
		DisableAutoGenTag:          true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			a.setStreams(a.g.noColor)
			a.status.quiet = a.quiet()
			f, err := a.format()
			if err != nil {
				return err.(*exitError).withCmd(cmd)
			}
			if f == formatSARIF && cmd.Annotations["sarif"] != "yes" {
				return usageErrorf("-o sarif is for `muscles images check` only: SARIF reports findings").withCmd(cmd)
			}
			if a.g.timeout > 0 {
				ctx, cancel := context.WithTimeout(cmd.Context(), a.g.timeout)
				cmd.SetContext(ctx)
				a.cancel = cancel
			}
			if cmd.Annotations["config"] == "skip" {
				return nil
			}
			return a.load(cmd)
		},
	}
	// Before the completion command exists: it keeps the writer it sees.
	root.SetIn(a.opts.Stdin)
	root.SetOut(a.out)
	root.SetErr(a.errw)
	root.SetVersionTemplate("muscles {{.Version}}\n")
	root.SetFlagErrorFunc(flagError)
	root.SetHelpFunc(a.help)
	root.SetUsageFunc(func(cmd *cobra.Command) error { a.help(cmd, nil); return nil })

	pf := root.PersistentFlags()
	pf.StringVar(&a.g.configFile, "config", "", "config file (default: nearest .muscles.yaml, plus $XDG_CONFIG_HOME/muscles/config.yaml)")
	pf.StringVarP(&a.g.output, "output", "o", "", "output format: table, plain, json or markdown (default: table on a terminal, plain otherwise)")
	pf.BoolVar(&a.g.json, "json", false, "shorthand for --output json; wins over --output")
	pf.String("source", "", "where images come from: auto, docker, remote, tar or oci (default auto)")
	bindFlag(pf, "source", "source")
	pf.String("platform", "", "platform of a multi-platform image, like linux/arm64 (default: linux on this machine's architecture)")
	bindFlag(pf, "platform", "platform")
	pf.String("dockerfile", "", "Dockerfile the image was built from, to point layers at its lines (config key dockerfile)")
	bindFlag(pf, "dockerfile", "dockerfile")
	pf.String("cache-dir", "", "layer index cache (default $XDG_CACHE_HOME/muscles)")
	bindFlag(pf, "cache-dir", "cache.dir")
	pf.BoolVar(&a.g.fresh, "fresh", false, "index every layer again instead of using the cache")
	pf.DurationVar(&a.g.timeout, "timeout", 0, "give up after this long, like 5m (default: no limit)")
	pf.BoolVar(&a.g.noColor, "no-color", false, "no color (also NO_COLOR=1, and off when not a terminal)")
	pf.BoolVarP(&a.g.quiet, "quiet", "q", false, "print only errors on stderr")
	pf.BoolVarP(&a.g.verbose, "verbose", "v", false, "print detail on stderr; wins over --quiet")
	_ = root.RegisterFlagCompletionFunc("output", fixed("table", "plain", "json", "markdown"))
	kinds := make([]string, len(source.Kinds))
	for i, k := range source.Kinds {
		kinds[i] = string(k)
	}
	_ = root.RegisterFlagCompletionFunc("source", fixed(kinds...))
	_ = root.RegisterFlagCompletionFunc("platform", fixed("linux/amd64", "linux/arm64", "linux/arm/v7", "linux/s390x", "linux/ppc64le"))

	root.AddGroup(
		&cobra.Group{ID: "explain", Title: "Explain an image"},
		&cobra.Group{ID: "compare", Title: "Compare and gate"},
		&cobra.Group{ID: "other", Title: "Settings and more"},
	)
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add("explain", newLayersCmd(a), newChangesCmd(a), newWasteCmd(a))
	add("compare", newImagesCmd(a))
	add("other", newCacheCmd(a), newConfigCmd(a), newVersionCmd(a), newManCmd(a))
	root.SetHelpCommand(newHelpCmd(a))
	root.SetHelpCommandGroupID("other")
	root.SetCompletionCommandGroupID("other")
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() == "completion" {
			c.Annotations = map[string]string{"config": "skip"}
			c.Example = "  muscles completion zsh > \"${fpath[1]}/_muscles\"\n  muscles completion bash > /etc/bash_completion.d/muscles\n  muscles completion fish > ~/.config/fish/completions/muscles.fish"
		}
	}
	return root
}

// fixed returns a completion function that offers vals and no files.
func fixed(vals ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return vals, cobra.ShellCompDirectiveNoFileComp
	}
}

// nounArgs rejects an unknown subcommand with a suggestion. A noun command
// with no arguments prints its help.
func nounArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	err := usageErrorf("unknown command %q for %q", args[0], cmd.CommandPath()).withCmd(cmd)
	if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
		err.err = fmt.Errorf("%w\nDid you mean %q?", err.err, s[0])
	}
	return err
}

// noArgs is cobra.NoArgs as a usage error.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageErrorf("%s takes no arguments, got %q", cmd.CommandPath(), strings.Join(args, " ")).withCmd(cmd)
	}
	return nil
}

// noun returns a command that groups verbs: run bare, it prints its help.
func noun(use, short, long string) *cobra.Command {
	return &cobra.Command{
		Use:         use,
		Short:       short,
		Long:        long,
		Args:        nounArgs,
		Annotations: map[string]string{"noun": "true", "config": "skip"},

		SuggestionsMinimumDistance: 2,
		RunE:                       func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
}

// flagError turns a flag parse error into a usage error, suggesting the
// closest flag name.
func flagError(cmd *cobra.Command, err error) error {
	ue := usageErrorf("%v", err).withCmd(cmd)
	msg := err.Error()
	if name, ok := strings.CutPrefix(msg, "unknown flag: --"); ok {
		var names []string
		cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
		sort.Strings(names)
		best, bestD := "", 3
		for _, n := range names {
			if d := config.Distance(name, n); d < bestD {
				best, bestD = n, d
			}
		}
		if best != "" {
			ue.err = fmt.Errorf("%v\nDid you mean --%s?", err, best)
		}
	}
	return ue
}
