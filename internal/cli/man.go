package cli

import (
	"bytes"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func newManCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "man",
		Short: "Print the muscles(1) man page",
		Long: `Print the muscles(1) man page in roff, generated from the command tree.
Release archives and the Homebrew package install it for you.`,
		Example: `  muscles man > muscles.1
  muscles man | gzip -9 > /usr/local/share/man/man1/muscles.1.gz
  muscles man | man -l -`,
		Args:        noArgs,
		Hidden:      true,
		Annotations: map[string]string{"config": "skip"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			var buf bytes.Buffer
			header := &doc.GenManHeader{Title: "MUSCLES", Section: "1", Source: "muscles", Manual: "muscles manual"}
			if err := doc.GenMan(cmd.Root(), header, &buf); err != nil {
				return err
			}
			_, err := a.opts.Stdout.Write(buf.Bytes())
			return err
		},
	}
}
