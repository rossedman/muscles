package cli

import "github.com/spf13/cobra"

func newImagesCmd(a *app) *cobra.Command {
	cmd := noun("images", "Compare two images, or check one against budgets",
		`Compare two images layer by layer and file by file, or check an image
against size, waste and growth budgets and forbidden paths, for CI.`)
	cmd.Example = `  muscles images diff ghcr.io/acme/app:1.8.0 app:dev
  muscles images check app.tar --baseline ghcr.io/acme/app:main
  muscles images diff ghcr.io/acme/app:main app.tar -o markdown >> "$GITHUB_STEP_SUMMARY"`
	cmd.AddCommand(newImagesDiffCmd(a), newImagesCheckCmd(a))
	return cmd
}
