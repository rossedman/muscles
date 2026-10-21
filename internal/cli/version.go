package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rossedman/muscles/internal/version"
)

// newVersionCmd builds `muscles version`, which prints the version, commit and platform.
func newVersionCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version, commit and platform",
		Long: `Print what a bug report needs: the version, the commit it was built from,
the Go version and the platform. With --json, print it as an object.`,
		Example: `  muscles version
  muscles version --json
  muscles version --json | jq -r .version`,
		Args:        noArgs,
		Annotations: map[string]string{"config": "skip"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			v := version.Get()
			if a.jsonOutput() {
				return a.printJSON(v)
			}
			commit := v.Commit
			if commit == "" {
				commit = "unknown"
			}
			_, err := fmt.Fprintf(a.out, "muscles %s (commit %s, %s, %s)\n", v.Version, commit, v.Go, v.Platform)
			return err
		},
	}
}
