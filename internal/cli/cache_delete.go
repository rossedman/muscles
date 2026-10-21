package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

type cacheDeleteJSON struct {
	Schema  string `json:"schema"`
	Preview bool   `json:"preview"`
	Layers  int    `json:"layers"`
	Bytes   int64  `json:"bytes"`
}

// newCacheDeleteCmd builds `muscles cache delete`, which clears the layer index cache or, with --preview, reports what it would clear.
func newCacheDeleteCmd(a *app) *cobra.Command {
	var preview bool
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete every cached layer index",
		Long: `Delete every cached layer index. Nothing is lost: the next command reads
the layers again. With --preview, report what would be deleted and change
nothing.`,
		Example: `  muscles cache delete --preview
  muscles cache delete
  muscles cache delete --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := a.cache()
			if c == nil {
				return usageErrorf("no cache directory: set cache.dir or --cache-dir").withCmd(cmd)
			}
			entries, err := c.List()
			if err != nil {
				return err
			}
			if !preview {
				if entries, err = c.Prune(0); err != nil {
					return err
				}
			}
			var total int64
			for _, e := range entries {
				total += e.Bytes
			}
			if a.jsonOutput() {
				return a.printJSON(cacheDeleteJSON{Schema: Schema, Preview: preview, Layers: len(entries), Bytes: total})
			}
			verb := "deleted"
			if preview {
				verb = "would delete"
			}
			_, err = fmt.Fprintf(a.out, "%s %d cached layers (%s) from %s\n", verb, len(entries), a.size(total), c.Dir)
			return err
		},
	}
	cmd.Flags().BoolVar(&preview, "preview", false, "show what would be deleted; change nothing")
	return cmd
}
