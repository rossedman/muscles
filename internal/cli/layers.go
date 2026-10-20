package cli

import "github.com/spf13/cobra"

func newLayersCmd(a *app) *cobra.Command {
	cmd := noun("layers", "Inspect an image's layers",
		`Inspect an image's layers: what each one stored, which Dockerfile step made
it, and what is worth a second look.`)
	cmd.Example = `  muscles layers list app:dev
  muscles layers list ghcr.io/acme/app:main --platform linux/arm64
  muscles layers list app.tar --json | jq '.layers[] | {index, size, step}'`
	cmd.AddCommand(newLayersListCmd(a))
	return cmd
}
