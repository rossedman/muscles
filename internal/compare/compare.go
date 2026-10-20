// Package compare diffs two analyzed images: the layers they share (by
// diffID, from the bottom up) and the changes between their final
// filesystems.
package compare

import (
	"path"
	"sort"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/index"
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
	return r
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
