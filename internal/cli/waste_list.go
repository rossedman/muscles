package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/size"
	"github.com/rossedman/muscles/internal/ui"
)

type wasteJSON struct {
	Schema string              `json:"schema"`
	Image  imageInfo           `json:"image"`
	Waste  int64               `json:"waste"`
	Items  []analysis.WasteRow `json:"items"`
}

// wasteReason says why bytes are hidden, in a few words.
var wasteReason = map[analysis.ChangeType]string{
	analysis.Meta:     "permission-only rewrite",
	analysis.Modified: "overwritten",
	analysis.Deleted:  "deleted",
}

func newWasteListCmd(a *app) *cobra.Command {
	var (
		files bool
		limit int
	)
	cmd := &cobra.Command{
		Use:   "list IMAGE",
		Short: "List wasted bytes, biggest first, with the layers responsible",
		Long: `List the bytes IMAGE stores but its final filesystem doesn't show, biggest
first, with the layer that stored them and the layer that hid them:

  overwritten              a later layer stored new contents at the path
  deleted                  a later layer deleted the path
  permission-only rewrite  a later layer stored the same contents again
                           (chown -R, chmod -R)

Fix waste by doing the work in the layer that created the files (for
example, clean the apt cache in the same RUN that filled it, or COPY
--chown instead of a later chown -R); ` + "`muscles images inspect`" + ` ranks
the fixes by what they save. Plain output has one line per row:
size, path, count, reason, stored-in layer, hidden-by layer.` + "\n" + imageHelp,
		Example: `  muscles waste list app:dev
  muscles waste list app:dev --files --limit 50
  muscles waste list app:dev -o plain | awk -F'\t' '$1 > 1000000'`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := a.args(cmd, args, "IMAGE")
			if err != nil {
				return err
			}
			r, err := a.analyzeBuilt(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if r.Waste > 0 {
				a.next("muscles images inspect "+shellQuote(args[0]), "the fixes, ranked by what they save")
			}
			if files {
				limit = 0
			}
			return a.printWaste(r, analysis.RollupWaste(r.WasteItems, limit))
		},
	}
	cmd.Flags().BoolVar(&files, "files", false, "list every file; don't roll up directories")
	cmd.Flags().IntVar(&limit, "limit", analysis.DefaultLimit, "about how many rows to roll up into")
	return cmd
}

func (a *app) printWaste(r *analysis.Report, rows []analysis.WasteRow) error {
	f, _ := a.format()
	if f == formatJSON {
		return a.printJSON(wasteJSON{Schema: Schema, Image: infoOf(r), Waste: r.Waste, Items: nonNil(rows)})
	}
	if len(rows) == 0 {
		if f == formatTable {
			fmt.Fprintf(a.out, "No waste in %s: every stored byte is in the final filesystem.\n", r.Ref)
		}
		return nil
	}
	layer := func(i int) ui.Cell {
		c := a.stepCell(fmt.Sprintf("L%d ", i), r.Layers[i])
		c.Paint = a.th.Painter(a.th.Muted)
		return c
	}
	t := &ui.Table{Columns: []ui.Column{{Title: "SIZE", Right: true}, {Title: "PATH"}, {Title: "REASON"}, {Title: "STORED IN", Max: 36}, {Title: "HIDDEN BY", Max: 36}}}
	if f == formatPlain {
		t.Columns = []ui.Column{{Title: "SIZE"}, {Title: "PATH"}, {Title: "COUNT"}, {Title: "REASON"}, {Title: "STORED IN"}, {Title: "HIDDEN BY"}}
	}
	for _, w := range rows {
		p := w.Path
		if w.Dir {
			p += "/"
		}
		reason := wasteReason[w.Reason]
		if f == formatPlain {
			t.Add(a.sizeCell(w.Size), ui.C(p), ui.C(fmt.Sprint(w.Count)), ui.C(reason), ui.C(fmt.Sprint(w.StoredIn)), ui.C(fmt.Sprint(w.HiddenBy)))
			continue
		}
		if w.Dir {
			reason += fmt.Sprintf(" (%d files)", w.Count)
		}
		t.Add(a.sizeCell(w.Size), ui.C(p), ui.Cell{Text: reason, Paint: a.th.Painter(a.th.Warn)}, layer(w.StoredIn), layer(w.HiddenBy))
	}
	if err := a.render(f, t, "Waste in "+r.Ref); err != nil {
		return err
	}
	if f == formatPlain {
		return nil
	}
	total := fmt.Sprintf("total waste %s (%s of %s)", a.size(r.Waste), size.Percent(r.Waste, r.Size), a.size(r.Size))
	_, err := fmt.Fprintf(a.out, "\n%s\n", a.th.Paint(a.th.Bold, total))
	return err
}
