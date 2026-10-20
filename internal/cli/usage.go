package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rossedman/muscles/internal/ui"
	"github.com/rossedman/muscles/internal/version"
)

// terse reports whether help was asked for with -h, which prints a short
// screen; --help and `muscles help` print everything.
func (a *app) terse() bool {
	for _, s := range a.argv {
		if s == "--" {
			return false
		}
		if s == "-h" {
			return true
		}
	}
	return false
}

func isNoun(cmd *cobra.Command) bool { return cmd.Annotations["noun"] == "true" }

// help renders a command's help on stdout: help that was asked for is data.
func (a *app) help(cmd *cobra.Command, _ []string) {
	th, terse := a.th, a.terse()
	width := ui.Width(a.opts.Stdout)
	if width <= 0 || width > 120 {
		width = min(max(width, 80), 120)
	}
	var b strings.Builder
	head := func(s string) { b.WriteString(th.Paint(th.Head, s) + "\n") }
	if !cmd.HasParent() && !terse && th.On {
		b.WriteString(ui.Banner(th, version.Get().Version) + "\n")
	}
	desc := cmd.Long
	if desc == "" || terse {
		desc = cmd.Short
	}
	b.WriteString(strings.TrimSpace(desc) + "\n\n")

	head("Usage:")
	if cmd.Runnable() && !isNoun(cmd) && cmd.HasParent() {
		b.WriteString("  " + cmd.UseLine() + "\n")
	}
	if cmd.HasAvailableSubCommands() {
		b.WriteString("  " + cmd.CommandPath() + " <command> [flags]\n")
	}
	b.WriteString("\n")

	if cmd.HasAvailableSubCommands() {
		width := 0
		for _, c := range cmd.Commands() {
			if c.IsAvailableCommand() && len(c.Name()) > width {
				width = len(c.Name())
			}
		}
		list := func(title string, match func(*cobra.Command) bool) {
			var rows []string
			for _, c := range cmd.Commands() {
				if c.IsAvailableCommand() && match(c) {
					pad := strings.Repeat(" ", width-len(c.Name()))
					rows = append(rows, fmt.Sprintf("  %s%s  %s", th.Paint(th.Cmd, c.Name()), pad, c.Short))
				}
			}
			if len(rows) > 0 {
				head(title)
				b.WriteString(strings.Join(rows, "\n") + "\n\n")
			}
		}
		if len(cmd.Groups()) > 0 {
			for _, g := range cmd.Groups() {
				list(g.Title+":", func(c *cobra.Command) bool { return c.GroupID == g.ID })
			}
		} else {
			list("Commands:", func(*cobra.Command) bool { return true })
		}
	}

	if !terse && cmd.Example != "" {
		head("Examples:")
		b.WriteString(strings.TrimRight(cmd.Example, "\n") + "\n\n")
	}
	if cmd.HasParent() {
		if fl := cmd.LocalFlags().FlagUsagesWrapped(width); fl != "" {
			head("Flags:")
			b.WriteString(fl + "\n")
		}
		if fl := cmd.InheritedFlags().FlagUsagesWrapped(width); fl != "" && !terse {
			head("Global flags:")
			b.WriteString(fl + "\n")
		}
	} else if fl := cmd.LocalFlags().FlagUsagesWrapped(width); fl != "" && !terse {
		head("Global flags:")
		b.WriteString(fl + "\n")
	}

	if terse {
		fmt.Fprintf(&b, "Run `%s --help` for examples and every flag.\n", cmd.CommandPath())
	} else {
		if !cmd.HasParent() {
			b.WriteString(exitCodesHelp + "\n")
		}
		fmt.Fprintf(&b, "Learn more: %s\nReport bugs: %s\n", docsURL, issuesURL)
	}
	fmt.Fprint(a.out, b.String())
}
