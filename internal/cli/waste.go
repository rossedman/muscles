package cli

import "github.com/spf13/cobra"

func newWasteCmd(a *app) *cobra.Command {
	cmd := noun("waste", "Find bytes an image stores but doesn't show",
		`Find waste: bytes stored in an image's layers that its final filesystem
doesn't show, because a later layer overwrote, deleted or re-stored them.`)
	cmd.Example = `  muscles waste list app:dev
  muscles waste list app:dev --files
  muscles waste list app:dev --json | jq '.waste'`
	cmd.AddCommand(newWasteListCmd(a))
	return cmd
}
