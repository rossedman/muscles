package cli

import "github.com/spf13/cobra"

func newImagesCmd(a *app) *cobra.Command {
	cmd := noun("images", "Inspect an image, compare two, or check one against budgets",
		`Say what to fix first in an image, compare two images layer by layer and
file by file, or check an image against size, waste and growth budgets and
forbidden paths, for CI.`)
	cmd.Example = `  muscles images inspect app:dev
  muscles images diff ghcr.io/acme/app:1.8.0 app:dev
  muscles images check app.tar --baseline ghcr.io/acme/app:main
  muscles images diff ghcr.io/acme/app:main app.tar -o markdown >> "$GITHUB_STEP_SUMMARY"`
	cmd.AddCommand(newImagesInspectCmd(a), newImagesDiffCmd(a), newImagesCheckCmd(a))
	return cmd
}
