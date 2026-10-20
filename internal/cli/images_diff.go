package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/compare"
	"github.com/rossedman/muscles/internal/pkgdb"
	"github.com/rossedman/muscles/internal/ui"
)

type diffLayerJSON struct {
	Index      int    `json:"index"`
	Shared     bool   `json:"shared"`
	Step       string `json:"step"`
	BaseSize   *int64 `json:"base_size"`
	TargetSize *int64 `json:"target_size"`
}

type diffJSON struct {
	Schema          string          `json:"schema"`
	Base            imageInfo       `json:"base"`
	Target          imageInfo       `json:"target"`
	SizeDelta       int64           `json:"size_delta"`
	CompressedDelta int64           `json:"compressed_delta"`
	SharedLayers    int             `json:"shared_layers"`
	Layers          []diffLayerJSON `json:"layers"`
	Changes         []analysis.Row  `json:"changes"`
	Summary         string          `json:"summary"`
	Packages        pkgdb.Diff      `json:"packages"`
}

func newImagesDiffCmd(a *app) *cobra.Command {
	var (
		files bool
		limit int
	)
	cmd := &cobra.Command{
		Use:   "diff BASE TARGET",
		Short: "Show what changed from one image to another",
		Long: `Show what changed from BASE to TARGET, starting with one sentence: the
size change, the step it came from, the biggest file changes and the
packages installed, upgraded or removed. Then the layers they share (identical from the bottom up, so pulls reuse them), the size of each
layer that differs, and the paths that differ in the final filesystems.

Directories whose changes are all alike are rolled up so the list fits
about --limit rows; --files lists every file. Plain output has one line per
filesystem change: type, size, path, count, note. With --dockerfile,
TARGET's layers show their Dockerfile lines.` + "\n" + imageHelp,
		Example: `  muscles images diff ghcr.io/acme/app:1.8.0 app:dev
  muscles images diff ghcr.io/acme/app:main app.tar -o markdown >> "$GITHUB_STEP_SUMMARY"
  muscles images diff app:1 app:2 --json | jq '.size_delta'`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := a.args(cmd, args, "BASE", "TARGET")
			if err != nil {
				return err
			}
			base, err := a.analyze(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			target, err := a.analyzeBuilt(cmd.Context(), args[1])
			if err != nil {
				return err
			}
			if files {
				limit = 0
			}
			r := compare.Images(base, target)
			if p, _, ok := r.BiggestLayerChange(); ok && p.Target != nil {
				a.next(fmt.Sprintf("muscles changes list %s --layer %d", shellQuote(args[1]), p.Index), "what changed in that layer")
			}
			return a.printDiff(r, limit)
		},
	}
	cmd.Flags().BoolVar(&files, "files", false, "list every file; don't roll up directories")
	cmd.Flags().IntVar(&limit, "limit", analysis.DefaultLimit, "about how many filesystem rows to roll up into")
	return cmd
}

func (a *app) printDiff(r *compare.Result, limit int) error {
	rows := analysis.RollupChanges(r.Changes, limit)
	f, _ := a.format()
	if f == formatJSON {
		out := diffJSON{Schema: Schema, Base: infoOf(r.Base), Target: infoOf(r.Target), SizeDelta: r.SizeDelta(), CompressedDelta: r.CompressedDelta(), SharedLayers: r.Shared, Layers: []diffLayerJSON{}, Changes: nonNil(rows), Summary: r.Verdict(a.cfg.IEC()),
			Packages: pkgdb.Diff{Installed: nonNil(r.Packages.Installed), Removed: nonNil(r.Packages.Removed), Upgraded: nonNil(r.Packages.Upgraded)}}
		for _, p := range r.Layers {
			l := diffLayerJSON{Index: p.Index, Shared: p.Shared, Step: p.Step()}
			if p.Base != nil {
				l.BaseSize = &p.Base.Size
			}
			if p.Target != nil {
				l.TargetSize = &p.Target.Size
			}
			out.Layers = append(out.Layers, l)
		}
		return a.printJSON(out)
	}

	fs := &ui.Table{Columns: []ui.Column{{Title: "TYPE"}, {Title: "SIZE", Right: true}, {Title: "PATH"}, {Title: "NOTE"}}}
	if f == formatPlain {
		fs.Columns = []ui.Column{{Title: "TYPE"}, {Title: "SIZE"}, {Title: "PATH"}, {Title: "COUNT"}, {Title: "NOTE"}}
	}
	dim := a.th.Painter(a.th.Muted)
	for _, row := range rows {
		p := row.Path
		if row.Dir {
			p += "/"
		}
		note := a.diffNote(row)
		cells := []ui.Cell{a.changeCell(row.Type), a.sizeCell(row.Size), {Text: p, Paint: a.pathPaint(row.Type)}}
		if f == formatPlain {
			cells = append(cells, ui.C(fmt.Sprint(row.Count)))
		}
		fs.Add(append(cells, ui.Cell{Text: note, Paint: dim})...)
	}
	if f == formatPlain {
		return fs.Plain(a.opts.Stdout)
	}

	pct := ""
	if r.Base.Size > 0 {
		pct = fmt.Sprintf(", %+.1f%%", float64(r.SizeDelta())*100/float64(r.Base.Size))
	}
	sizes := fmt.Sprintf("%s (%s%s) · compressed %s (%s)", a.size(r.Target.Size), a.delta(r.SizeDelta()), pct, a.size(r.Target.Compressed), a.delta(r.CompressedDelta()))
	layers := &ui.Table{Columns: []ui.Column{{Title: ""}, {Title: "#", Right: true}, {Title: "STEP", Max: 52}, {Title: "SIZE", Right: true}, {Title: "CHANGE", Right: true}}}
	for _, p := range r.Layers {
		mark, paint := "≠", a.th.Painter(a.th.Modified)
		if p.Shared {
			mark, paint = "=", dim
		}
		var sz, change string
		switch {
		case p.Shared:
			sz = a.size(p.Target.Size)
		case p.Base == nil:
			sz, change = "→ "+a.size(p.Target.Size), "new"
		case p.Target == nil:
			sz, change = a.size(p.Base.Size)+" →", "removed"
		default:
			sz, change = a.size(p.Base.Size)+" → "+a.size(p.Target.Size), a.delta(p.Target.Size-p.Base.Size)
		}
		step := ui.C(p.Step())
		if p.Target != nil {
			step = a.stepCell("", *p.Target)
		}
		layers.Add(ui.Cell{Text: mark, Paint: paint}, ui.C(fmt.Sprint(p.Index)), step, ui.C(sz), ui.Cell{Text: change, Paint: paint})
	}
	pkgs := a.packagesTable(r.Packages)
	verdict := r.Verdict(a.cfg.IEC())

	var err error
	if f == formatMarkdown {
		fmt.Fprintf(a.out, "### %s → %s\n\n%s\n\n%s\n\n", r.Base.Ref, r.Target.Ref, verdict, sizes)
		fmt.Fprintf(a.out, "**Layers** (%d of %d shared)\n\n", r.Shared, len(r.Layers))
		if err = layers.Markdown(a.out); err != nil {
			return err
		}
		if pkgs != nil {
			fmt.Fprintf(a.out, "\n**Packages** (%d changes)\n\n", len(pkgs.Rows))
			if err = pkgs.Markdown(a.out); err != nil {
				return err
			}
		}
		fmt.Fprintf(a.out, "\n**Filesystem** (%d changes)\n\n", len(r.Changes))
		if len(rows) == 0 {
			_, err = fmt.Fprintln(a.out, "No differences.")
			return err
		}
		if len(rows) > 15 {
			fmt.Fprintf(a.out, "<details><summary>%d rows</summary>\n\n", len(rows))
			defer fmt.Fprintln(a.out, "\n</details>")
		}
		return fs.Markdown(a.out)
	}
	fmt.Fprintf(a.out, "%s\n%s\n\n", a.th.Paint(a.th.Bold, verdict), a.th.Paint(a.th.Muted, r.Base.Ref+" → "+r.Target.Ref+"   "+sizes))
	fmt.Fprintf(a.out, "%s\n", a.th.Paint(a.th.Head, fmt.Sprintf("LAYERS (%d of %d shared)", r.Shared, len(r.Layers))))
	if err = layers.Render(a.out, a.th); err != nil {
		return err
	}
	if pkgs != nil {
		fmt.Fprintf(a.out, "\n%s\n", a.th.Paint(a.th.Head, fmt.Sprintf("PACKAGES (%d changes)", len(pkgs.Rows))))
		if err = pkgs.Render(a.out, a.th); err != nil {
			return err
		}
	}
	fmt.Fprintf(a.out, "\n%s\n", a.th.Paint(a.th.Head, fmt.Sprintf("FILESYSTEM (%d changes)", len(r.Changes))))
	if len(rows) == 0 {
		_, err = fmt.Fprintln(a.out, "No differences in the final filesystem.")
		return err
	}
	return fs.Render(a.out, a.th)
}

// packagesTable lists package changes, or is nil when there are none.
func (a *app) packagesTable(d pkgdb.Diff) *ui.Table {
	if d.Empty() {
		return nil
	}
	t := &ui.Table{Columns: []ui.Column{{Title: "TYPE"}, {Title: "PACKAGE"}, {Title: "VERSION"}, {Title: "MANAGER"}}}
	for _, p := range d.Installed {
		t.Add(a.changeCell(analysis.Added), ui.C(p.Name), ui.C(p.Version), ui.Cell{Text: p.Manager, Paint: a.th.Painter(a.th.Muted)})
	}
	for _, u := range d.Upgraded {
		t.Add(a.changeCell(analysis.Modified), ui.C(u.Name), ui.C(u.From+" → "+u.To), ui.Cell{Text: u.Manager, Paint: a.th.Painter(a.th.Muted)})
	}
	for _, p := range d.Removed {
		t.Add(a.changeCell(analysis.Deleted), ui.Cell{Text: p.Name, Paint: a.th.Painter(a.th.Muted)}, ui.C(p.Version), ui.Cell{Text: p.Manager, Paint: a.th.Painter(a.th.Muted)})
	}
	return t
}

func (a *app) diffNote(row analysis.Row) string {
	switch {
	case row.Dir:
		return fmt.Sprintf("%d files", row.Count)
	case row.Type == analysis.Modified:
		return fmt.Sprintf("was %s", a.size(row.PrevSize))
	case row.Type == analysis.Meta:
		return "same contents, new permissions or owner"
	}
	return ""
}
