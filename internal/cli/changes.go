package cli

import "github.com/spf13/cobra"

func newChangesCmd(a *app) *cobra.Command {
	cmd := noun("changes", "Inspect the files each layer changed",
		`Inspect the files each layer added, modified, stored again with the same
contents, or deleted.`)
	cmd.Example = `  muscles changes list app:dev --layer 4
  muscles changes list app:dev --type deleted
  muscles changes list app:dev --path /usr/local --files --json`
	cmd.AddCommand(newChangesListCmd(a))
	return cmd
}
