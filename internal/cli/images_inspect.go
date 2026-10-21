package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/rossedman/muscles/internal/advice"
	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/size"
	"github.com/rossedman/muscles/internal/ui"
)

type biggestJSON struct {
	Index   int    `json:"index"`
	Step    string `json:"step"`
	Line    int    `json:"dockerfile_line,omitempty"`
	Size    int64  `json:"size"`
	Summary string `json:"summary"`
}

type inspectJSON struct {
	Schema string    `json:"schema"`
	Image  imageInfo `json:"image"`
	advice.Advice
	Biggest []biggestJSON `json:"biggest_layers"`
}

// biggestLayers is how many layers inspect shows by size.
const biggestLayers = 3

// newImagesInspectCmd builds `muscles images inspect`, which says what to fix first in an image and how much each fix saves.
func newImagesInspectCmd(a *app) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "inspect IMAGE",
		Short: "Say what to fix first, and how much each fix saves",
		Long: `Explain IMAGE in one screen: its size, what it costs to pull, how much of
it is waste, the fixes that save the most (each with the layer, the
Dockerfile step and line, the problem and the change to make), files worth
a look, and the biggest layers.

Every wasted byte is accounted for, whatever the warn thresholds, and
counted by one fix only, so savings add up; --limit caps how many fixes are
shown. ` + "`muscles IMAGE`" + ` runs this command. Pass --dockerfile to point each fix at its line.
Plain output has one line per fix:
rank, saves, layer, dockerfile line, step, problem, fix.` + "\n" + imageHelp,
		Example: `  muscles app:dev
  muscles images inspect app:dev --dockerfile Dockerfile
  muscles images inspect ghcr.io/acme/app:main -o markdown >> "$GITHUB_STEP_SUMMARY"`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := a.args(cmd, args, "IMAGE")
			if err != nil {
				return err
			}
			a.everyFix = true
			r, err := a.analyzeBuilt(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			adv := advice.For(r)
			if len(adv.Fixes) > 0 {
				a.next(fmt.Sprintf("muscles changes list %s --layer %d", shellQuote(args[0]), adv.Fixes[0].Layer), "the files behind fix 1")
			} else {
				a.next("muscles layers list "+shellQuote(args[0]), "every layer and what it changed")
			}
			return a.printInspect(r, adv, limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 5, "show at most this many fixes in table and Markdown output; 0 shows all")
	return cmd
}

// biggest returns the largest layers, biggest first.
func biggest(r *analysis.Report, n int) []analysis.Layer {
	ls := append([]analysis.Layer{}, r.Layers...)
	sort.SliceStable(ls, func(i, j int) bool { return ls[i].Size > ls[j].Size })
	var out []analysis.Layer
	for _, l := range ls {
		if len(out) == n || l.Size == 0 {
			break
		}
		out = append(out, l)
	}
	return out
}

// printInspect writes the image summary, advice and biggest layers in the chosen output format.
func (a *app) printInspect(r *analysis.Report, adv advice.Advice, limit int) error {
	f, _ := a.format()
	big := biggest(r, biggestLayers)
	if f == formatJSON {
		out := inspectJSON{Schema: Schema, Image: infoOf(r), Advice: adv, Biggest: []biggestJSON{}}
		for _, l := range big {
			out.Biggest = append(out.Biggest, biggestJSON{Index: l.Index, Step: l.Step, Line: l.Line, Size: l.Size, Summary: l.Summary})
		}
		return a.printJSON(out)
	}
	if f == formatPlain {
		t := &ui.Table{}
		for i, x := range adv.Fixes {
			t.Add(ui.C(fmt.Sprint(i+1)), ui.C(fmt.Sprint(x.Saves)), ui.C(fmt.Sprint(x.Layer)), ui.C(fmt.Sprint(x.Line)), ui.C(x.Step), ui.C(x.Problem), ui.C(x.Fix))
		}
		return t.Plain(a.opts.Stdout)
	}

	shown, more := adv.Fixes, ""
	if limit > 0 && len(shown) > limit {
		shown = shown[:limit]
		var rest int64
		for _, x := range adv.Fixes[limit:] {
			rest += x.Saves
		}
		more = fmt.Sprintf("%d smaller fixes save %s more; --limit 0 shows them.", len(adv.Fixes)-limit, a.size(rest))
		if len(adv.Fixes)-limit == 1 {
			more = fmt.Sprintf("1 smaller fix saves %s more; --limit 0 shows it.", a.size(rest))
		}
	}
	header := fmt.Sprintf("%s · %s · %s to pull · %s wasted (%s) · %s",
		r.Ref, a.size(r.Size), a.size(r.Compressed), a.size(r.Waste), size.Percent(r.Waste, r.Size), plural(len(r.Layers), "layer"))
	layerOf := func(i int, step string, line int) string {
		return fmt.Sprintf("L%d %s", i, a.stepWithLine(analysis.Layer{Step: step, Line: line}))
	}
	w := a.out
	if f == formatMarkdown {
		fmt.Fprintf(w, "### %s\n\n%s\n\n", r.Ref, header)
		if len(adv.Fixes) == 0 {
			fmt.Fprintln(w, "No waste worth fixing.")
		} else {
			fmt.Fprintf(w, "**Fix these first** (saves %s)\n\n", a.size(adv.Saves))
			for i, x := range shown {
				fmt.Fprintf(w, "%d. **Saves %s** in `%s`: %s %s\n", i+1, a.size(x.Saves), layerOf(x.Layer, x.Step, x.Line), x.Problem, x.Fix)
			}
			if more != "" {
				fmt.Fprintf(w, "\n%s\n", more)
			}
		}
		if len(adv.Looks) > 0 {
			fmt.Fprintf(w, "\n**Worth a look**\n\n")
			for _, x := range adv.Looks {
				fmt.Fprintf(w, "- `%s`: %s\n", layerOf(x.Layer, x.Step, x.Line), x.Problem)
			}
		}
		if len(big) > 0 {
			fmt.Fprintf(w, "\n**Biggest layers**\n\n")
			for _, l := range big {
				fmt.Fprintf(w, "- %s `%s`: %s\n", a.size(l.Size), layerOf(l.Index, l.Step, l.Line), l.Summary)
			}
		}
		return nil
	}

	head := func(s string) { fmt.Fprintf(w, "\n%s\n", a.th.Paint(a.th.Head, s)) }
	dim := a.th.Painter(a.th.Muted)
	fmt.Fprintln(w, a.th.Paint(a.th.Bold, header))
	if len(adv.Fixes) == 0 {
		fmt.Fprintf(w, "\n%s\n", a.th.Paint(a.th.OK, "✓ No waste worth fixing."))
	} else {
		head(fmt.Sprintf("FIX THESE FIRST (saves %s)", a.size(adv.Saves)))
		for i, x := range shown {
			fmt.Fprintf(w, "%2d  %s  %s\n", i+1, a.th.Paint(a.th.Warn, "saves "+a.size(x.Saves)), layerOf(x.Layer, x.Step, x.Line))
			fmt.Fprintf(w, "    %s\n", a.wrap(x.Problem, 4, 4))
			fmt.Fprintf(w, "    %s\n", a.th.Paint(a.th.Accent, a.wrap("→ "+x.Fix, 4, 6)))
		}
		if more != "" {
			fmt.Fprintf(w, "    %s\n", dim(a.wrap(more, 4, 4)))
		}
	}
	if len(adv.Looks) > 0 {
		head("WORTH A LOOK")
		for _, x := range adv.Looks {
			fmt.Fprintf(w, " ·  %s  %s\n", x.Problem, dim(layerOf(x.Layer, x.Step, x.Line)))
		}
	}
	if len(big) > 0 {
		head("BIGGEST LAYERS")
		t := &ui.Table{Columns: []ui.Column{{Title: "#", Right: true}, {Title: "SIZE", Right: true}, {Title: "STEP", Max: 52}, {Title: "NOTE", Max: 64}}}
		for _, l := range big {
			t.Add(ui.C(fmt.Sprint(l.Index)), a.sizeCell(l.Size), a.stepCell("", l), ui.Cell{Text: l.Summary, Paint: dim})
		}
		return t.Render(w, a.th)
	}
	return nil
}
