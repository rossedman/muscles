package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/size"
	"github.com/rossedman/muscles/internal/ui"
)

type changeRow struct {
	Layer int `json:"layer"`
	analysis.Row
}

type changesJSON struct {
	Schema  string      `json:"schema"`
	Image   imageInfo   `json:"image"`
	Layer   *int        `json:"layer"`
	Changes []changeRow `json:"changes"`
}

// changeFilter is what `changes list` selects.
type changeFilter struct {
	layer   int
	types   []string
	path    string
	minSize string
	files   bool
	limit   int
}

func newChangesListCmd(a *app) *cobra.Command {
	var cf changeFilter
	cmd := &cobra.Command{
		Use:   "list IMAGE",
		Short: "List the paths layers added, modified, rewrote or deleted",
		Long: `List the paths that changed in one layer (--layer) or in every layer,
biggest first:

  +  added      the path didn't exist below this layer
  ~  modified   the path existed below with different contents or type
  ◌  meta       the same contents stored again; only permissions, owner or
                times changed (the old copy is still stored: waste)
  -  deleted    a whiteout removed it; its size is what disappeared from
                the filesystem, not what was freed (lower layers keep it)

Directories whose changes are all alike are rolled up into one row so the
list fits about --limit rows, opening the biggest directories first;
--files lists every file. Plain output has one line per row:
layer, type, size, path, count, note.` + "\n" + imageHelp,
		Example: `  muscles changes list app:dev --layer 4
  muscles changes list app:dev --type deleted,meta --min-size 1MB
  muscles changes list app:dev --path /usr/local --files -o plain | sort -t$'\t' -k3 -n`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := a.args(cmd, args, "IMAGE")
			if err != nil {
				return err
			}
			minSize, err := cf.validate()
			if err != nil {
				return err.(*exitError).withCmd(cmd)
			}
			r, err := a.analyze(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if cf.layer >= len(r.Layers) {
				return usageErrorf("--layer %d: %s has %d layers (0 to %d)", cf.layer, r.Ref, len(r.Layers), len(r.Layers)-1).withCmd(cmd)
			}
			return a.printChanges(r, cf, minSize)
		},
	}
	fl := cmd.Flags()
	fl.IntVar(&cf.layer, "layer", -1, "only this layer (default: every layer)")
	fl.StringSliceVar(&cf.types, "type", nil, "only these change types: added, modified, meta, deleted")
	fl.StringVar(&cf.path, "path", "", "only paths at or below this one, like /usr/local")
	fl.StringVar(&cf.minSize, "min-size", "", "only rows at least this big, like 1MB")
	fl.BoolVar(&cf.files, "files", false, "list every file; don't roll up directories")
	fl.IntVar(&cf.limit, "limit", analysis.DefaultLimit, "about how many rows to roll up into per layer")
	_ = cmd.RegisterFlagCompletionFunc("type", fixed("added", "modified", "meta", "deleted"))
	return cmd
}

func (cf changeFilter) validate() (int64, error) {
	for _, t := range cf.types {
		ok := false
		for _, ct := range analysis.ChangeTypes {
			ok = ok || t == string(ct)
		}
		if !ok {
			return 0, usageErrorf("invalid --type %q (use added, modified, meta or deleted)", t)
		}
	}
	if cf.layer < -1 {
		return 0, usageErrorf("invalid --layer %d", cf.layer)
	}
	if cf.minSize == "" {
		return 0, nil
	}
	n, err := size.ParseBytes(cf.minSize)
	if err != nil {
		return 0, usageErrorf("--min-size: %v", err)
	}
	return n, nil
}

func (cf changeFilter) keep(c analysis.Change) bool {
	if len(cf.types) > 0 {
		ok := false
		for _, t := range cf.types {
			ok = ok || string(c.Type) == t
		}
		if !ok {
			return false
		}
	}
	if p := strings.TrimSuffix(cf.path, "/"); p != "" && c.Path != p && !strings.HasPrefix(c.Path, p+"/") {
		return false
	}
	return true
}

func (a *app) printChanges(r *analysis.Report, cf changeFilter, minSize int64) error {
	limit := cf.limit
	if cf.files {
		limit = 0
	}
	rows := []changeRow{}
	for _, l := range r.Layers {
		if cf.layer >= 0 && l.Index != cf.layer {
			continue
		}
		var cs []analysis.Change
		for _, c := range l.Changes {
			if cf.keep(c) {
				cs = append(cs, c)
			}
		}
		for _, row := range analysis.RollupChanges(cs, limit) {
			if row.Size >= minSize {
				rows = append(rows, changeRow{Layer: l.Index, Row: row})
			}
		}
	}
	f, _ := a.format()
	if f == formatJSON {
		out := changesJSON{Schema: Schema, Image: infoOf(r), Changes: rows}
		if cf.layer >= 0 {
			out.Layer = &cf.layer
		}
		return a.printJSON(out)
	}
	if len(rows) == 0 {
		if f == formatTable {
			where := "any layer"
			if cf.layer >= 0 {
				where = fmt.Sprintf("layer %d (%s)", cf.layer, r.Layers[cf.layer].Step)
			}
			fmt.Fprintf(a.out, "No changes in %s match.\n", where)
		}
		return nil
	}
	t := &ui.Table{Columns: []ui.Column{{Title: "L", Right: true}, {Title: "TYPE"}, {Title: "SIZE", Right: true}, {Title: "PATH"}, {Title: "NOTE"}}}
	if f == formatPlain {
		t.Columns = []ui.Column{{Title: "L"}, {Title: "TYPE"}, {Title: "SIZE"}, {Title: "PATH"}, {Title: "COUNT"}, {Title: "NOTE"}}
	}
	dim := a.th.Painter(a.th.Muted)
	for _, row := range rows {
		p := row.Path
		if row.Dir {
			p += "/"
		}
		cells := []ui.Cell{ui.C(fmt.Sprint(row.Layer)), a.changeCell(row.Type), a.sizeCell(row.Size), {Text: p, Paint: a.pathPaint(row.Type)}}
		if f == formatPlain {
			cells = append(cells, ui.C(fmt.Sprint(row.Count)))
		}
		t.Add(append(cells, ui.Cell{Text: a.changeNote(row.Row), Paint: dim})...)
	}
	title := "Changes in " + r.Ref
	if cf.layer >= 0 {
		title = fmt.Sprintf("Changes in layer %d of %s", cf.layer, r.Ref)
	}
	return a.render(f, t, title)
}

func (a *app) pathPaint(t analysis.ChangeType) func(string) string {
	if t == analysis.Deleted {
		return a.th.Painter(a.th.Muted)
	}
	return nil
}

// changeNote explains a row: what it replaced, or where the bytes still are.
func (a *app) changeNote(row analysis.Row) string {
	var parts []string
	if row.Dir {
		parts = append(parts, fmt.Sprintf("%d files", row.Count))
	}
	switch row.Type {
	case analysis.Modified:
		if row.PrevLayer >= 0 && !row.Dir {
			parts = append(parts, fmt.Sprintf("was %s (L%d)", a.size(row.PrevSize), row.PrevLayer))
		}
	case analysis.Meta:
		if row.PrevLayer >= 0 {
			parts = append(parts, fmt.Sprintf("same contents as L%d", row.PrevLayer))
		}
	case analysis.Deleted:
		if row.Size > 0 && row.PrevLayer >= 0 {
			parts = append(parts, fmt.Sprintf("bytes still in L%d", row.PrevLayer))
		}
	}
	return strings.Join(parts, ", ")
}
