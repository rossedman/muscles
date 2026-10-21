package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rossedman/muscles/internal/index"
	"github.com/rossedman/muscles/internal/ui"
)

type cacheJSON struct {
	Schema   string             `json:"schema"`
	Dir      string             `json:"dir"`
	Bytes    int64              `json:"bytes"`
	MaxBytes int64              `json:"max_bytes"`
	Layers   []index.CacheEntry `json:"layers"`
}

// newCacheListCmd builds `muscles cache list`, which lists cached layer indexes, most recently used first.
func newCacheListCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List cached layer indexes, most recently used first",
		Long: `List the cached layer indexes, most recently used first, with their size
on disk. Plain output has one line per layer: diffID, bytes, last used.`,
		Example: `  muscles cache list
  muscles cache list --json | jq '.bytes'
  muscles cache list -o plain | wc -l`,
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
			var total int64
			for _, e := range entries {
				total += e.Bytes
			}
			f, _ := a.format()
			if f == formatJSON {
				return a.printJSON(cacheJSON{Schema: Schema, Dir: c.Dir, Bytes: total, MaxBytes: c.MaxBytes, Layers: nonNil(entries)})
			}
			if len(entries) == 0 {
				if f == formatTable {
					fmt.Fprintf(a.out, "The cache in %s is empty.\n", c.Dir)
				}
				return nil
			}
			t := &ui.Table{Columns: []ui.Column{{Title: "LAYER"}, {Title: "SIZE", Right: true}, {Title: "USED"}}}
			for _, e := range entries {
				t.Add(ui.C(e.DiffID), a.sizeCell(e.Bytes), ui.C(e.Used.Format("2006-01-02 15:04")))
			}
			if err := a.render(f, t, "Layer cache"); err != nil {
				return err
			}
			if f != formatPlain {
				fmt.Fprintf(a.out, "\n%d layers · %s of %s in %s\n", len(entries), a.size(total), a.size(c.MaxBytes), c.Dir)
			}
			return nil
		},
	}
}
