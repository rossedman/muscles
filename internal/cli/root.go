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
	root.SetArgs(argv)
	err := root.ExecuteContext(ctx)
	if a.cancel != nil {
		a.cancel()
	}
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return a.printError(err)
	}
	a.status.clear()
	if !a.quiet() {
		for _, h := range a.hints {
			fmt.Fprintln(a.errw, a.eth.Paint(a.eth.Muted, h))
		}
	}
	return ExitOK
}

const rootLong = `muscles explains container images: what each layer added, changed and
deleted, where the wasted space is, and what changed between two versions.
It reads images from the local Docker daemon, a registry (without pulling),
a docker save tarball or an OCI layout, and runs the same on a laptop and
in CI.

Nothing leaves your machine except reads from the registries you name.
Credentials come from your Docker config and credential helpers, never
from flags.`

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
			if _, err := a.format(); err != nil {
				return err.(*exitError).withCmd(cmd)
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
