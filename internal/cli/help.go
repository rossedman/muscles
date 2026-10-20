package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagSchema describes a flag in `help --json`.
type flagSchema struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Default   string `json:"default"`
	Usage     string `json:"usage"`
}

// cmdSchema describes a command in `help --json`.
type cmdSchema struct {
	Name     string       `json:"name"`
	Path     string       `json:"path"`
	Short    string       `json:"short"`
	Usage    string       `json:"usage"`
	Examples []string     `json:"examples"`
	Flags    []flagSchema `json:"flags"`
	Global   []flagSchema `json:"global_flags,omitempty"`
	Commands []cmdSchema  `json:"commands"`
}

func flagsOf(fs *pflag.FlagSet) []flagSchema {
	out := []flagSchema{}
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		out = append(out, flagSchema{Name: f.Name, Shorthand: f.Shorthand, Type: f.Value.Type(), Default: f.DefValue, Usage: f.Usage})
	})
	return out
}

func schemaOf(cmd *cobra.Command) cmdSchema {
	s := cmdSchema{Name: cmd.Name(), Path: cmd.CommandPath(), Short: cmd.Short, Usage: cmd.UseLine(), Examples: []string{}, Commands: []cmdSchema{}}
	for _, line := range strings.Split(cmd.Example, "\n") {
		if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "#") {
			s.Examples = append(s.Examples, l)
		}
	}
	if cmd.HasParent() {
		s.Flags = flagsOf(cmd.LocalFlags())
	} else {
		s.Flags, s.Global = []flagSchema{}, flagsOf(cmd.PersistentFlags())
	}
	for _, c := range cmd.Commands() {
		if c.IsAvailableCommand() {
			s.Commands = append(s.Commands, schemaOf(c))
		}
	}
	return s
}

func newHelpCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "Show help for any command, or the command tree as JSON",
		Long: `Show the full help for a command: what it does, examples and every flag.
With --json, print the command tree (names, usage, examples and flags) as
JSON, generated from the same definitions as this help.`,
		Example: `  muscles help images check
  muscles help --json | jq '.commands[].path'
  muscles help layers list --json`,
		Annotations: map[string]string{"config": "skip"},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			target, _, _ := cmd.Root().Find(args)
			var names []string
			for _, c := range target.Commands() {
				if c.IsAvailableCommand() {
					names = append(names, c.Name())
				}
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			target, rest, err := cmd.Root().Find(args)
			if err != nil || len(rest) > 0 {
				return usageErrorf("no help for %q: unknown command", strings.Join(args, " ")).withCmd(cmd)
			}
			if a.g.json {
				return a.printJSON(schemaOf(target))
			}
			a.help(target, nil)
			return nil
		},
	}
}
