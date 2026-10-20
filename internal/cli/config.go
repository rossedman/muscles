package cli

import "github.com/spf13/cobra"

func newConfigCmd(a *app) *cobra.Command {
	cmd := noun("config", "Show settings and where each one came from",
		`Show muscles' settings. Each one is resolved in this order, first match
wins: a flag, an environment variable (MUSCLES_CHECK_MAX_SIZE for
check.max_size), the project file (.muscles.yaml, found by searching up
from the working directory; --config names another), the user file
($XDG_CONFIG_HOME/muscles/config.yaml), the default.`)
	cmd.Example = `  muscles config list
  muscles config list --json | jq '.settings[] | select(.origin != "default")'
  MUSCLES_CHECK_MAX_SIZE=1GB muscles config list`
	cmd.AddCommand(newConfigListCmd(a))
	return cmd
}
