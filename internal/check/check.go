// Package check evaluates an analyzed image against size, waste and growth
// budgets and forbidden paths, and turns layer notes into warnings.
package check

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/size"
)

// Status is a rule's outcome.
type Status string

// Rule outcomes.
const (
	Pass Status = "pass"
	Fail Status = "fail"
	Warn Status = "warn"
	Skip Status = "skip"
)

// Symbol is the outcome's marker; the word is always printed too.
func (s Status) Symbol() string {
	switch s {
	case Pass:
		return "✓"
	case Fail:
		return "✗"
	case Warn:
		return "⚠"
	}
	return "·"
}

// Budget is what an image must stay within. Zero values are not checked.
type Budget struct {
	MaxSize       int64
	MaxCompressed int64
	MaxWaste      size.Limit // bytes, or a percentage of the image size
	MaxGrowth     size.Limit // bytes, or a percentage of the baseline size
	MaxLayers     int
	Forbid        []string // globs ("**/.env"); a trailing / matches directories only
	Allow         []string // exceptions to Forbid
}

// Validate reports a malformed glob.
func (b Budget) Validate() error {
	for _, p := range append(append([]string{}, b.Forbid...), b.Allow...) {
		if !doublestar.ValidatePattern(strings.TrimSuffix(strings.TrimPrefix(p, "/"), "/")) {
			return fmt.Errorf("invalid path pattern %q", p)
		}
	}
	return nil
}

// Result is one rule's outcome.
type Result struct {
	Rule    string `json:"rule"`
	Status  Status `json:"status"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	Fix     string `json:"fix,omitempty"`             // what to change, for a fail or warn
	Layer   *int   `json:"layer,omitempty"`           // the layer at fault, when there is one
	Line    int    `json:"dockerfile_line,omitempty"` // that layer's Dockerfile line, when known
}

// Report is the outcome of every rule.
type Report struct {
	Image    string   `json:"image"`
	Baseline string   `json:"baseline,omitempty"`
	Results  []Result `json:"results"`
	Failed   int      `json:"failed"`
	Warnings int      `json:"warnings"`
}

// OK reports whether no rule failed.
func (r Report) OK() bool { return r.Failed == 0 }

// Run checks img (and its growth over base, if base is not nil).
func Run(img, base *analysis.Report, b Budget, iec bool) Report {
	f := func(n int64) string { return size.Format(n, iec) }
	rep := Report{Image: img.Ref}
	add := func(r Result) {
		if r.Layer != nil && *r.Layer < len(img.Layers) {
			r.Line = img.Layers[*r.Layer].Line
		}
		rep.Results = append(rep.Results, r)
		switch r.Status {
		case Fail:
			rep.Failed++
		case Warn:
			rep.Warnings++
		}
	}
	inspect := fmt.Sprintf("Run `muscles images inspect %s` for the fixes that save the most.", img.Ref)
	limit := func(rule string, got, max int64, what string) {
		if got <= max {
			add(Result{Rule: rule, Status: Pass, Message: fmt.Sprintf("%s ≤ %s", f(got), f(max))})
		} else {
			add(Result{Rule: rule, Status: Fail, Message: fmt.Sprintf("%s > %s", f(got), f(max)), Detail: what, Fix: inspect})
		}
	}
	if b.MaxSize > 0 {
		limit("size", img.Size, b.MaxSize, biggestLayer(img, iec))
	}
	if b.MaxCompressed > 0 {
		limit("compressed", img.Compressed, b.MaxCompressed, "")
	}
	if !b.MaxWaste.IsZero() {
		limit("waste", img.Waste, b.MaxWaste.Resolve(img.Size), biggestWaste(img, iec))
	}
	if !b.MaxGrowth.IsZero() {
		if base == nil {
			add(Result{Rule: "growth", Status: Skip, Message: "no baseline image", Detail: "pass --baseline IMAGE to check growth"})
		} else {
			rep.Baseline = base.Ref
			delta, max := img.Size-base.Size, b.MaxGrowth.Resolve(base.Size)
			r := Result{Rule: "growth", Status: Pass, Message: fmt.Sprintf("%s ≤ +%s vs %s", size.FormatDelta(delta, iec), f(max), base.Ref)}
			if delta > max {
				r.Status, r.Message = Fail, fmt.Sprintf("%s > +%s vs %s", size.FormatDelta(delta, iec), f(max), base.Ref)
				r.Fix = fmt.Sprintf("Run `muscles images diff %s %s` to see what grew.", base.Ref, img.Ref)
			}
			add(r)
		}
	}
	if b.MaxLayers > 0 {
		n := len(img.Layers)
		if n <= b.MaxLayers {
			add(Result{Rule: "layers", Status: Pass, Message: fmt.Sprintf("%d ≤ %d", n, b.MaxLayers)})
		} else {
			add(Result{Rule: "layers", Status: Fail, Message: fmt.Sprintf("%d > %d", n, b.MaxLayers), Fix: "Combine consecutive RUN steps, or build in stages and copy only the result."})
		}
	}
	for _, pat := range b.Forbid {
		hits := Forbidden(img, pat, b.Allow)
		if len(hits) == 0 {
			add(Result{Rule: "forbid", Status: Pass, Message: fmt.Sprintf("nothing matches %q", pat)})
			continue
		}
		for i, h := range hits {
			if i == 5 {
				add(Result{Rule: "forbid", Status: Fail, Message: fmt.Sprintf("%d more paths match %q", len(hits)-5, pat)})
				break
			}
			e := img.Final[h]
			layer := e.Layer
			add(Result{Rule: "forbid", Status: Fail, Message: fmt.Sprintf("%s matches %q", h, pat), Detail: fmt.Sprintf("added in L%d %s", e.Layer, img.Layers[e.Layer].Step),
				Fix: fmt.Sprintf("Keep it out of the build context with .dockerignore, or stop adding it in L%d.", e.Layer), Layer: &layer})
		}
	}
	for _, l := range img.Layers {
		for _, n := range l.Notes {
			layer := l.Index
			add(Result{Rule: strings.ReplaceAll(n.Kind, "_", " "), Status: Warn, Message: n.Message, Detail: fmt.Sprintf("L%d %s", l.Index, l.Step), Fix: n.Fix, Layer: &layer})
		}
	}
	return rep
}

// Forbidden lists final paths that match pattern and no allow pattern,
// sorted. Patterns may start with / (anchored either way) and end with / to
// match directories only.
func Forbidden(img *analysis.Report, pattern string, allow []string) []string {
	var out []string
	for p, e := range img.Final {
		if matches(pattern, p, e.Type.String() == "dir") && !anyMatch(allow, p, e.Type.String() == "dir") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func anyMatch(pats []string, p string, dir bool) bool {
	for _, a := range pats {
		if matches(a, p, dir) {
			return true
		}
	}
	return false
}

func matches(pattern, p string, dir bool) bool {
	if strings.HasSuffix(pattern, "/") {
		if !dir {
			return false
		}
		pattern = strings.TrimSuffix(pattern, "/")
	}
	ok, _ := doublestar.Match(strings.TrimPrefix(pattern, "/"), strings.TrimPrefix(p, "/"))
	return ok
}

func biggestLayer(img *analysis.Report, iec bool) string {
	best := -1
	for i, l := range img.Layers {
		if best < 0 || l.Size > img.Layers[best].Size {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	l := img.Layers[best]
	return fmt.Sprintf("biggest layer: L%d %s (%s)", l.Index, l.Step, size.Format(l.Size, iec))
}

func biggestWaste(img *analysis.Report, iec bool) string {
	rows := analysis.RollupWaste(img.WasteItems, 5)
	if len(rows) == 0 {
		return ""
	}
	w := rows[0]
	why := map[analysis.ChangeType]string{
		analysis.Meta:     "permission-only rewrite",
		analysis.Modified: "overwritten",
		analysis.Deleted:  "deleted",
	}[w.Reason]
	p := w.Path
	if w.Dir {
		p += "/"
	}
	return fmt.Sprintf("biggest: %s (%s, %s in L%d)", p, size.Format(w.Size, iec), why, w.HiddenBy)
}
