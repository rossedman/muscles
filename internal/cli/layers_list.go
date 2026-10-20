package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/size"
	"github.com/rossedman/muscles/internal/ui"
)

// imageHelp explains image references; every command that takes one
// includes it.
const imageHelp = `
IMAGE is a name in the local Docker daemon (app:dev), a registry reference
(ghcr.io/acme/app:main, read without pulling), a docker save tarball
(app.tar) or an OCI layout directory (./out). Force one with a prefix:
docker://, registry://, tar:// or oci://DIR[:tag], or with --source.`

type layersJSON struct {
	Schema     string      `json:"schema"`
	Image      imageInfo   `json:"image"`
	Layers     []layerJSON `json:"layers"`
	EmptyAfter []string    `json:"empty_steps_after"`
}

type layerJSON struct {
	analysis.Layer
	Notes       []analysis.Note     `json:"notes"`
	Mostly      []analysis.DirBytes `json:"mostly"`
	EmptyBefore []string            `json:"empty_steps_before"`
}

func newLayersListCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "list IMAGE",
		Short: "List each layer: size, changes, Dockerfile step and notes",
		Long: `List each layer of IMAGE, bottom to top: its uncompressed size, how many
files it added (+), modified (~), stored again with the same contents (◌)
and deleted (-), the Dockerfile step that made it, and a note: where most of
its bytes went, or a warning worth acting on (a package cache left behind,
a deletion that frees nothing, a permission-only rewrite, a large file).

Steps that make no layer (ENV, LABEL, CMD, ...) are shown dimmed between
layers. Plain output has one line per layer:
index, size, compressed, added, modified, meta, deleted, step, note.` + "\n" + imageHelp,
		Example: `  muscles layers list app:dev
  muscles layers list ghcr.io/acme/app:main -o markdown >> "$GITHUB_STEP_SUMMARY"
  muscles layers list app.tar --json | jq '.image.waste'`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := a.args(cmd, args, "IMAGE")
			if err != nil {
				return err
			}
			r, err := a.analyze(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.printLayers(r)
		},
	}
}

func (a *app) printLayers(r *analysis.Report) error {
	f, _ := a.format()
	if f == formatJSON {
		out := layersJSON{Schema: Schema, Image: infoOf(r), Layers: []layerJSON{}, EmptyAfter: nonNil(r.EmptyAfter)}
		for _, l := range r.Layers {
			out.Layers = append(out.Layers, layerJSON{Layer: l, Notes: nonNil(l.Notes), Mostly: nonNil(l.Mostly), EmptyBefore: nonNil(l.EmptyBefore)})
		}
		return a.printJSON(out)
	}
	if len(r.Layers) == 0 {
		if f == formatTable {
			fmt.Fprintf(a.out, "No layers in %s.\n", r.Ref)
		}
		return nil
	}
	t := &ui.Table{Columns: []ui.Column{
		{Title: "#", Right: true}, {Title: "SIZE", Right: true}, {Title: "+", Right: true}, {Title: "~", Right: true},
		{Title: "◌", Right: true}, {Title: "-", Right: true}, {Title: "STEP", Max: 52}, {Title: "NOTE", Max: 64},
	}}
	if f == formatPlain {
		t.Columns = append(t.Columns[:2], append([]ui.Column{{Title: "COMPRESSED"}}, t.Columns[2:]...)...)
	}
	dim := a.th.Painter(a.th.Muted)
	count := func(n int, paint func(string) string) ui.Cell {
		if n == 0 {
			return ui.Cell{Text: "·", Plain: "0", Paint: dim}
		}
		return ui.Cell{Text: fmt.Sprintf("%d", n), Paint: paint}
	}
	for _, l := range r.Layers {
		if f != formatPlain {
			for _, s := range l.EmptyBefore {
				t.Add(ui.C(""), ui.C(""), ui.C(""), ui.C(""), ui.C(""), ui.Cell{Text: "·", Paint: dim}, ui.Cell{Text: s, Paint: dim}, ui.C(""))
			}
		}
		note := ui.Cell{Text: l.Summary, Paint: dim}
		if len(l.Notes) > 0 {
			text := "⚠ " + l.Notes[0].Message
			if len(l.Notes) > 1 {
				text += fmt.Sprintf(" (+%d more)", len(l.Notes)-1)
			}
			note = ui.Cell{Text: text, Plain: l.Notes[0].Message, Paint: a.th.Painter(a.th.Warn)}
		}
		step := l.Step
		if step == "" {
			step = "?"
		}
		cells := []ui.Cell{
			ui.C(fmt.Sprint(l.Index)), a.sizeCell(l.Size),
			count(l.Counts.Added, a.th.Painter(a.th.Added)), count(l.Counts.Modified, a.th.Painter(a.th.Modified)),
			count(l.Counts.Meta, a.th.Painter(a.th.Meta)), count(l.Counts.Deleted, a.th.Painter(a.th.Deleted)),
			ui.C(step), note,
		}
		if f == formatPlain {
			cells = append(cells[:2], append([]ui.Cell{ui.C(fmt.Sprint(l.Compressed))}, cells[2:]...)...)
		}
		t.Add(cells...)
	}
	if f != formatPlain {
		for _, s := range r.EmptyAfter {
			t.Add(ui.C(""), ui.C(""), ui.C(""), ui.C(""), ui.C(""), ui.Cell{Text: "·", Paint: dim}, ui.Cell{Text: s, Paint: dim}, ui.C(""))
		}
	}
	if err := a.render(f, t, "Layers of "+r.Ref); err != nil {
		return err
	}
	if f == formatPlain {
		return nil
	}
	total := fmt.Sprintf("total %s uncompressed · %s compressed · waste %s (%s)", a.size(r.Size), a.size(r.Compressed), a.size(r.Waste), size.Percent(r.Waste, r.Size))
	if f == formatMarkdown {
		_, err := fmt.Fprintf(a.out, "\n%s\n", total)
		return err
	}
	_, err := fmt.Fprintf(a.out, "\n%s\n", a.th.Paint(a.th.Bold, total))
	return err
}

// nonNil returns an empty slice for nil, so JSON says [] rather than null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
