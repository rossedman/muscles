// Package advice turns an analysis into a short, ranked list of fixes: what
// to change in which layer, and how many bytes it saves. Each wasted byte is
// counted once, by the first fix that claims it, so savings add up. It does
// no I/O.
package advice

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/size"
)

// Fix is one change to make.
type Fix struct {
	Kind    string `json:"kind"` // an analysis note kind
	Layer   int    `json:"layer"`
	Step    string `json:"step"`
	Line    int    `json:"dockerfile_line,omitempty"`
	Saves   int64  `json:"saves"`
	Problem string `json:"problem"`
	Fix     string `json:"fix"`
}

// Advice is what to fix, biggest saving first, and what is worth a look
// but has no measurable saving.
type Advice struct {
	Fixes []Fix `json:"fixes"`
	Looks []Fix `json:"worth_a_look"`
	Saves int64 `json:"saves"`
}

type claim struct {
	path  string
	layer int
}

// For builds the advice for r.
func For(r *analysis.Report) Advice {
	adv := Advice{Fixes: []Fix{}, Looks: []Fix{}}
	claimed := map[claim]bool{}
	byHider := map[int][]analysis.WasteItem{}
	for _, w := range r.WasteItems {
		byHider[w.HiddenBy] = append(byHider[w.HiddenBy], w)
	}
	unclaimed := func(layer int, reason analysis.ChangeType) (int64, int) {
		var b int64
		n := 0
		for _, w := range byHider[layer] {
			k := claim{w.Path, w.StoredIn}
			if w.Reason == reason && !claimed[k] {
				claimed[k] = true
				b += w.Size
				n++
			}
		}
		return b, n
	}
	fix := func(l analysis.Layer, n analysis.Note, saves int64, problem string) Fix {
		return Fix{Kind: n.Kind, Layer: l.Index, Step: l.Step, Line: l.Line, Saves: saves, Problem: problem, Fix: n.Fix}
	}

	// Package caches first: they are the clearest fix, and a later layer
	// deleting the same files should not count them again.
	for _, l := range r.Layers {
		for _, n := range l.Notes {
			if n.Kind != analysis.NotePackageCache {
				continue
			}
			var saves int64
			deletedIn := map[int]bool{}
			for _, c := range l.Changes {
				if c.Type == analysis.Deleted || c.Kind != "file" || !strings.HasPrefix(c.Path, n.Path+"/") {
					continue
				}
				saves += c.Size
				claimed[claim{c.Path, l.Index}] = true
			}
			for _, w := range r.WasteItems {
				if w.StoredIn == l.Index && w.Reason == analysis.Deleted && strings.HasPrefix(w.Path, n.Path+"/") {
					deletedIn[w.HiddenBy] = true
				}
			}
			label, _, _ := analysis.CacheFix(n.Path)
			problem := fmt.Sprintf("Leaves the %s in the image (%s).", label, filesAndBytes(n.Files, saves))
			if len(deletedIn) > 0 {
				problem += fmt.Sprintf(" %s deletes them later, but that frees nothing.", layerNames(deletedIn))
			}
			adv.Fixes = append(adv.Fixes, fix(l, n, saves, problem))
		}
	}
	for _, l := range r.Layers {
		for _, n := range l.Notes {
			switch n.Kind {
			case analysis.NotePermissionRewrite:
				b, k := unclaimed(l.Index, analysis.Meta)
				if b > 0 {
					adv.Fixes = append(adv.Fixes, fix(l, n, b, fmt.Sprintf("Stores %s again only to change owner or permissions; the first copy is still in %s.", filesAndBytes(k, b), storedIn(n.StoredIn))))
				}
			case analysis.NoteOverwrite:
				b, k := unclaimed(l.Index, analysis.Modified)
				if b > 0 {
					adv.Fixes = append(adv.Fixes, fix(l, n, b, fmt.Sprintf("Replaces %s that %s still stores.", filesAndBytes(k, b), storedIn(n.StoredIn))))
				}
			case analysis.NoteHiddenDelete:
				b, _ := unclaimed(l.Index, analysis.Deleted)
				if b > 0 {
					adv.Fixes = append(adv.Fixes, fix(l, n, b, fmt.Sprintf("Deletes %s that %s still stores, so the image doesn't get smaller.", size.Format(b, false), storedIn(n.StoredIn))))
				}
			case analysis.NoteLargeFile:
				adv.Looks = append(adv.Looks, fix(l, n, 0, fmt.Sprintf("A single %s file: %s.", size.Format(n.Bytes, false), n.Path)))
			}
		}
	}
	sort.SliceStable(adv.Fixes, func(i, j int) bool {
		if adv.Fixes[i].Saves != adv.Fixes[j].Saves {
			return adv.Fixes[i].Saves > adv.Fixes[j].Saves
		}
		return adv.Fixes[i].Layer < adv.Fixes[j].Layer
	})
	for _, f := range adv.Fixes {
		adv.Saves += f.Saves
	}
	return adv
}

// filesAndBytes formats a file count and byte total, like "3 files, 1.2 MB".
func filesAndBytes(n int, b int64) string {
	if n == 1 {
		return "1 file, " + size.Format(b, false)
	}
	return fmt.Sprintf("%d files, %s", n, size.Format(b, false))
}

// storedIn names the layers that stored something, like "L2, L5".
func storedIn(ls []int) string { return analysis.LayerNames(ls) }

// layerNames sorts the layer set and names it like storedIn does.
func layerNames(set map[int]bool) string {
	var ls []int
	for l := range set {
		ls = append(ls, l)
	}
	sort.Ints(ls)
	return analysis.LayerNames(ls)
}
