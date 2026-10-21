// Package compare diffs two analyzed images: the layers they share (by
// diffID, from the bottom up) and the changes between their final
// filesystems.
package compare

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/index"
	"github.com/rossedman/muscles/internal/pkgdb"
	"github.com/rossedman/muscles/internal/size"
)

// LayerPair lines up layer Index of both images. Base or Target is nil when
// one image has fewer layers.
type LayerPair struct {
	Index  int
	Shared bool
	Base   *analysis.Layer
	Target *analysis.Layer
}

// Step is the target's step, or the base's when the target has no layer.
func (p LayerPair) Step() string {
	if p.Target != nil {
		return p.Target.Step
	}
	return p.Base.Step
}

// Result is the comparison of base and target.
type Result struct {
	Base, Target *analysis.Report
	Shared       int
	Layers       []LayerPair
	Changes      []analysis.Change // final filesystem, base → target
	Packages     pkgdb.Diff        // installed packages, base → target
}

// SizeDelta is the change in uncompressed size.
func (r *Result) SizeDelta() int64 { return r.Target.Size - r.Base.Size }

// CompressedDelta is the change in compressed size.
func (r *Result) CompressedDelta() int64 { return r.Target.Compressed - r.Base.Compressed }

// Images compares base and target.
func Images(base, target *analysis.Report) *Result {
	r := &Result{Base: base, Target: target}
	n := max(len(base.Layers), len(target.Layers))
	shared := true
	for i := 0; i < n; i++ {
		p := LayerPair{Index: i}
		if i < len(base.Layers) {
			p.Base = &base.Layers[i]
		}
		if i < len(target.Layers) {
			p.Target = &target.Layers[i]
		}
		if shared && p.Base != nil && p.Target != nil && p.Base.DiffID == p.Target.DiffID {
			p.Shared = true
			r.Shared++
		} else {
			shared = false
		}
		r.Layers = append(r.Layers, p)
	}
	r.Changes = Filesystems(base.Final, target.Final)
	r.Packages = pkgdb.Compare(base.Packages, target.Packages)
	return r
}

// Verdict says what changed in a sentence or two: "app:2 is 38 MB bigger
// than app:1 (+10.5%), mostly in RUN pip install (+42 MB). Biggest
// changes: + /usr/.../pandas (61 MB), - /usr/.../simplejson (1 MB).
// Packages: installed curl 8.5 (dpkg)."
func (r *Result) Verdict(iec bool) string {
	f := func(n int64) string { return size.Format(n, iec) }
	d := r.SizeDelta()
	var b strings.Builder
	switch {
	case d == 0:
		fmt.Fprintf(&b, "%s is the same size as %s", r.Target.Ref, r.Base.Ref)
	case d > 0:
		fmt.Fprintf(&b, "%s is %s bigger than %s", r.Target.Ref, f(d), r.Base.Ref)
	default:
		fmt.Fprintf(&b, "%s is %s smaller than %s", r.Target.Ref, f(-d), r.Base.Ref)
	}
	if d != 0 && r.Base.Size > 0 {
		fmt.Fprintf(&b, " (%+.1f%%)", float64(d)*100/float64(r.Base.Size))
	}
	if p, delta, ok := r.BiggestLayerChange(); ok && d != 0 {
		fmt.Fprintf(&b, ", mostly in %s (%s)", p.Step(), size.FormatDelta(delta, iec))
	}
	b.WriteString(".")
	rows := analysis.RollupChanges(r.Changes, 8)
	impact := func(row analysis.Row) int64 {
		switch row.Type {
		case analysis.Added:
			return row.Size
		case analysis.Deleted:
			return -row.Size
		case analysis.Modified:
			return row.Size - row.PrevSize
		}
		return 0
	}
	sort.SliceStable(rows, func(i, j int) bool { return abs(impact(rows[i])) > abs(impact(rows[j])) })
	var top []string
	for _, row := range rows {
		if len(top) == 2 || impact(row) == 0 {
			break
		}
		p := row.Path
		if row.Dir {
			p += "/"
		}
		amount := f(abs(impact(row)))
		if row.Type == analysis.Modified {
			amount = size.FormatDelta(impact(row), iec)
		}
		top = append(top, fmt.Sprintf("%s %s (%s)", row.Type.Symbol(), p, amount))
	}
	if len(top) > 0 {
		b.WriteString(" Biggest changes: " + strings.Join(top, ", ") + ".")
	}
	if !r.Packages.Empty() {
		b.WriteString(" Packages: " + r.Packages.Summary() + ".")
	}
	return b.String()
}

// BiggestLayerChange returns the layer whose size changed the most, and by
// how much. A layer only one image has counts as all added or all removed.
func (r *Result) BiggestLayerChange() (LayerPair, int64, bool) {
	var best LayerPair
	var bestD int64
	found := false
	for _, p := range r.Layers {
		if p.Shared {
			continue
		}
		var d int64
		if p.Target != nil {
			d += p.Target.Size
		}
		if p.Base != nil {
			d -= p.Base.Size
		}
		if !found || abs(d) > abs(bestD) {
			best, bestD, found = p, d, true
		}
	}
	return best, bestD, found
}

// abs returns the absolute value of n.
func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// Filesystems lists what changed from a to b. Directories only appear when
// they were added or removed with nothing inside, or changed type.
func Filesystems(a, b map[string]analysis.FinalEntry) []analysis.Change {
	pa, pb := parents(a), parents(b)
	var out []analysis.Change
	for p, eb := range b {
		ea, ok := a[p]
		switch {
		case !ok:
			if eb.Type == index.Dir && pb[p] {
				continue
			}
			out = append(out, analysis.Change{Path: p, Type: analysis.Added, Kind: eb.Type.String(), Size: eb.Size, PrevLayer: -1})
		case ea.Type != eb.Type || ea.Hash != eb.Hash || ea.Size != eb.Size || ea.Link != eb.Link:
			out = append(out, analysis.Change{Path: p, Type: analysis.Modified, Kind: eb.Type.String(), Size: eb.Size, PrevSize: ea.Size, PrevLayer: ea.Layer})
		case ea.Mode != eb.Mode || ea.UID != eb.UID || ea.GID != eb.GID:
			if eb.Type != index.Dir {
				out = append(out, analysis.Change{Path: p, Type: analysis.Meta, Kind: eb.Type.String(), Size: eb.Size, PrevSize: ea.Size, PrevLayer: ea.Layer})
			}
		}
	}
	for p, ea := range a {
		if _, ok := b[p]; ok {
			continue
		}
		if ea.Type == index.Dir && pa[p] {
			continue
		}
		out = append(out, analysis.Change{Path: p, Type: analysis.Deleted, Kind: ea.Type.String(), Size: ea.Size, PrevLayer: ea.Layer})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// parents is the set of directories that hold something.
func parents(fs map[string]analysis.FinalEntry) map[string]bool {
	out := make(map[string]bool, len(fs)/4)
	for p := range fs {
		out[path.Dir(p)] = true
	}
	return out
}
