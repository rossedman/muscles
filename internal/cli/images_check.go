package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/check"
	"github.com/rossedman/muscles/internal/ui"
)

type checkJSON struct {
	Schema string `json:"schema"`
	OK     bool   `json:"ok"`
	check.Report
	Results []check.Result `json:"results"`
}

func newImagesCheckCmd(a *app) *cobra.Command {
	var baseline string
	cmd := &cobra.Command{
		Use:   "check IMAGE",
		Short: "Check an image against budgets and forbidden paths, for CI",
		Long: `Check IMAGE against the budgets in .muscles.yaml (or the flags below) and
exit 5 if any fails. Checks that aren't configured are skipped; layer notes
(package caches, deletions that free nothing, permission-only rewrites,
large files) are reported as warnings and never fail.

  size        uncompressed size          check.max_size / --max-size
  compressed  compressed size (pulled)   check.max_compressed / --max-compressed
  waste       bytes hidden by later      check.max_waste / --max-waste
              layers: bytes or % of size
  growth      size change vs --baseline: check.max_growth / --max-growth
              bytes or % of the baseline
  layers      number of layers           check.max_layers / --max-layers
  forbid      glob patterns that must    check.forbid (check.allow for
              not exist, like **/.env    exceptions); a trailing / means
                                         directories only

When GITHUB_ACTIONS is set, table and plain output also print ::error and
::warning workflow commands, so results show up on the run; with
--dockerfile they point at the line that made the layer. Every failure and
warning says how to fix it. Plain output has one line per rule:
status, rule, result, detail, fix, dockerfile line (0 if unknown).` + "\n" + imageHelp,
		Example: `  muscles images check app:dev
  muscles images check app.tar --baseline ghcr.io/acme/app:main --max-growth 5%
  muscles images check app.tar --max-size 800MB --max-waste 50MB --json`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			args, err := a.args(cmd, args, "IMAGE")
			if err != nil {
				return err
			}
			budget, err := a.cfg.Budget()
			if err != nil {
				return usageErrorf("%v", err).withCmd(cmd)
			}
			img, err := a.analyzeBuilt(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			var base *analysis.Report
			if baseline != "" {
				if base, err = a.analyze(cmd.Context(), baseline); err != nil {
					return err
				}
			}
			rep := check.Run(img, base, budget, a.cfg.IEC())
			if err := a.printCheck(rep); err != nil {
				return err
			}
			if !rep.OK() {
				a.next("muscles images inspect "+shellQuote(args[0]), "the fixes that save the most")
				return &exitError{code: ExitCheckFailed, kind: "CHECK_FAILED", err: fmt.Errorf("%d of %d checks failed", rep.Failed, len(rep.Results)-rep.Warnings)}
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&baseline, "baseline", "", "image to measure growth against, like the main branch's image")
	fl.String("max-size", "", "fail above this uncompressed size, like 800MB")
	bindFlag(fl, "max-size", "check.max_size")
	fl.String("max-compressed", "", "fail above this compressed size, like 300MB")
	bindFlag(fl, "max-compressed", "check.max_compressed")
	fl.String("max-waste", "", "fail above this much waste: bytes or a percentage, like 50MB or 10%")
	bindFlag(fl, "max-waste", "check.max_waste")
	fl.String("max-growth", "", "fail when the image grew more than this vs --baseline: bytes or a percentage")
	bindFlag(fl, "max-growth", "check.max_growth")
	fl.Int("max-layers", 0, "fail above this many layers")
	bindFlag(fl, "max-layers", "check.max_layers")
	return cmd
}

func (a *app) printCheck(rep check.Report) error {
	f, _ := a.format()
	if f == formatJSON {
		return a.printJSON(checkJSON{Schema: Schema, OK: rep.OK(), Report: rep, Results: nonNil(rep.Results)})
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" && (f == formatTable || f == formatPlain) {
		for _, r := range rep.Results {
			level := map[check.Status]string{check.Fail: "error", check.Warn: "warning"}[r.Status]
			if level == "" {
				continue
			}
			msg := r.Message
			if r.Detail != "" {
				msg += " (" + r.Detail + ")"
			}
			if r.Fix != "" {
				msg += "\nFix: " + r.Fix
			}
			where := ""
			if r.Line > 0 {
				where = fmt.Sprintf("file=%s,line=%d,", ghProperty(workspacePath(a.cfg.Dockerfile)), r.Line)
			}
			fmt.Fprintf(a.opts.Stdout, "::%s %stitle=muscles %s::%s\n", level, where, r.Rule, ghEscape(msg))
		}
	}
	if len(rep.Results) == 0 {
		if f == formatTable {
			fmt.Fprintf(a.out, "No checks configured for %s and no warnings.\nSet budgets in .muscles.yaml, or pass --max-size, --max-waste, --max-growth or --max-layers.\n", rep.Image)
		}
		return nil
	}
	paint := map[check.Status]func(string) string{
		check.Pass: a.th.Painter(a.th.OK), check.Fail: a.th.Painter(a.th.Err),
		check.Warn: a.th.Painter(a.th.Warn), check.Skip: a.th.Painter(a.th.Muted),
	}
	t := &ui.Table{Columns: []ui.Column{{Title: "STATUS"}, {Title: "RULE"}, {Title: "RESULT"}, {Title: "DETAIL", Max: 60}}}
	for _, r := range rep.Results {
		cells := []ui.Cell{{Text: r.Status.Symbol() + " " + string(r.Status), Plain: string(r.Status), Paint: paint[r.Status]}, ui.C(r.Rule), ui.C(r.Message), {Text: r.Detail, Paint: a.th.Painter(a.th.Muted)}}
		if f == formatPlain {
			cells = append(cells, ui.C(r.Fix), ui.C(fmt.Sprint(r.Line)))
		}
		t.Add(cells...)
	}
	if err := a.render(f, t, "Checks for "+rep.Image); err != nil {
		return err
	}
	if f == formatPlain {
		return nil
	}
	a.printCheckFixes(f, rep, paint)
	summary := fmt.Sprintf("%d failed, %s", rep.Failed, plural(rep.Warnings, "warning"))
	if rep.OK() {
		summary = "all checks passed, " + plural(rep.Warnings, "warning")
	}
	_, err := fmt.Fprintf(a.out, "\n%s\n", a.th.Paint(a.th.Bold, summary))
	return err
}

// printCheckFixes lists how to fix each failure and warning, once per
// distinct fix.
func (a *app) printCheckFixes(f string, rep check.Report, paint map[check.Status]func(string) string) {
	seen := map[string]bool{}
	var lines []string
	for _, r := range rep.Results {
		if r.Fix == "" || seen[r.Rule+r.Fix] {
			continue
		}
		seen[r.Rule+r.Fix] = true
		where := ""
		if r.Line > 0 {
			where = fmt.Sprintf(" (%s:%d)", filepath.Base(a.cfg.Dockerfile), r.Line)
		} else if r.Layer != nil {
			where = fmt.Sprintf(" (L%d)", *r.Layer)
		}
		if f == formatMarkdown {
			lines = append(lines, fmt.Sprintf("- **%s**%s: %s", r.Rule, where, r.Fix))
			continue
		}
		lead := fmt.Sprintf("%s%s: ", r.Rule, where)
		lines = append(lines, fmt.Sprintf(" %s %s%s", paint[r.Status](r.Status.Symbol()), lead, a.wrap(r.Fix, 3+len(lead), 3)))
	}
	if len(lines) == 0 {
		return
	}
	if f == formatMarkdown {
		fmt.Fprintf(a.out, "\n**How to fix**\n\n%s\n", strings.Join(lines, "\n"))
		return
	}
	fmt.Fprintf(a.out, "\n%s\n%s\n", a.th.Paint(a.th.Head, "HOW TO FIX"), strings.Join(lines, "\n"))
}

// plural is "1 warning" or "2 warnings".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// workspacePath makes path relative to the repository checkout
// ($GITHUB_WORKSPACE), the form GitHub needs to place an annotation on a
// pull request's diff. Paths outside the checkout are left as they are.
func workspacePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	ws := os.Getenv("GITHUB_WORKSPACE")
	if ws == "" {
		ws, _ = os.Getwd()
	}
	if rel, err := filepath.Rel(ws, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
		return filepath.ToSlash(rel)
	}
	return path
}

// ghProperty escapes a workflow command's property value.
func ghProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}

// ghEscape escapes a workflow command's message.
func ghEscape(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}
