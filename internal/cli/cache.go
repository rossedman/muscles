package cli

import "github.com/spf13/cobra"

func newCacheCmd(a *app) *cobra.Command {
	cmd := noun("cache", "Inspect or clear the layer index cache",
		`Inspect or clear the layer index cache. Layers are identified by content,
so a cached index never goes stale: a base image is read once and reused by
every image built on it. The cache lives in $XDG_CACHE_HOME/muscles (or
cache.dir) and is capped at cache.max_size, least recently used first.`)
	cmd.Example = `  muscles cache list
  muscles cache delete --preview
  muscles cache delete`
	cmd.AddCommand(newCacheListCmd(a), newCacheDeleteCmd(a))
	return cmd
}
