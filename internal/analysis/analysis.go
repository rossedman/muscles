// Package analysis replays an image's layers over a merged filesystem and
// classifies every entry: added, modified, rewritten with the same contents
// (meta), or deleted. It attributes waste (bytes stored but not visible in
// the final image), pairs layers with build steps and writes a short
// summary and notes for each layer. It does no I/O.
package analysis

import (
	"path"
	"sort"
	"strings"
	"time"

	"github.com/rossedman/muscles/internal/index"
	"github.com/rossedman/muscles/internal/pkgdb"
)

// ChangeType classifies a change to a path.
type ChangeType string

// The change types.
const (
	Added    ChangeType = "added"
	Modified ChangeType = "modified"
	Meta     ChangeType = "meta" // same contents stored again: permissions, owner or times changed
	Deleted  ChangeType = "deleted"
)

// ChangeTypes lists the change types in display order.
var ChangeTypes = []ChangeType{Added, Modified, Meta, Deleted}

// Symbol is the one-character marker for a change type.
func (c ChangeType) Symbol() string {
	switch c {
	case Added:
		return "+"
	case Modified:
		return "~"
	case Meta:
		return "◌"
	}
	return "-"
}

// Change is one path a layer changed.
type Change struct {
	Path      string     `json:"path"`
	Type      ChangeType `json:"type"`
	Kind      string     `json:"kind"`
	Size      int64      `json:"size"`
	PrevSize  int64      `json:"prev_size"`
	PrevLayer int        `json:"prev_layer"` // layer that stored the previous version; -1 if none
}

// WasteItem is a file whose bytes are stored in one layer and hidden by a
// later one.
type WasteItem struct {
	Path     string     `json:"path"`
	Size     int64      `json:"size"`
	StoredIn int        `json:"stored_in"`
	HiddenBy int        `json:"hidden_by"`
	Reason   ChangeType `json:"reason"`
}

// Counts are a layer's changes by type.
type Counts struct {
	Added    int `json:"added"`
	Modified int `json:"modified"`
	Meta     int `json:"meta"`
	Deleted  int `json:"deleted"`
}

// Note kinds.
const (
	NotePackageCache      = "package_cache"
	NoteHiddenDelete      = "hidden_delete"
	NotePermissionRewrite = "permission_rewrite"
	NoteLargeFile         = "large_file"
	NoteOverwrite         = "overwrite"
)

// Note is something in a layer worth a warning.
type Note struct {
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	Bytes    int64  `json:"bytes"`
	Files    int    `json:"files,omitempty"`
	Path     string `json:"path,omitempty"`
	StoredIn []int  `json:"stored_in,omitempty"` // layers that still store the bytes
	Fix      string `json:"fix"`                 // what to change, in one sentence
}

// DirBytes is a directory and the changed bytes under it.
type DirBytes struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// Layer is the analysis of one layer.
type Layer struct {
	Index       int         `json:"index"`
	DiffID      string      `json:"diff_id"`
	Digest      string      `json:"digest"`
	Size        int64       `json:"size"`
	Compressed  int64       `json:"compressed"`
	Step        string      `json:"step"`
	Line        int         `json:"dockerfile_line,omitempty"` // the Dockerfile line of Step, when known
	Created     time.Time   `json:"created"`
	EmptyBefore []string    `json:"empty_steps_before"`
	Counts      Counts      `json:"counts"`
	Wasted      int64       `json:"wasted"` // bytes stored here that a later layer hid
	Mostly      []DirBytes  `json:"mostly"`
	Summary     string      `json:"summary"`
	Notes       []Note      `json:"notes"`
	Packages    *pkgdb.Diff `json:"packages,omitempty"` // packages this layer installed, upgraded or removed
	Changes     []Change    `json:"-"`
}

// FinalEntry is a path in the final filesystem and the layer that stored it.
type FinalEntry struct {
	index.Entry
	Layer int
}

// Report is the analysis of an image.
type Report struct {
	Ref        string
	Source     string
	Platform   string
	Digest     string
	Size       int64 // all bytes stored, across layers
	Compressed int64
	Waste      int64
	Layers     []Layer
	EmptyAfter []string // empty steps after the last layer (CMD, ENTRYPOINT, ...)
	WasteItems []WasteItem
	Final      map[string]FinalEntry
	Packages   pkgdb.Set // installed in the final image, from dpkg, apk, rpm and Python metadata
}

// Options tune the notes.
type Options struct {
	LargeFile         int64 // note single files at least this big; 0 disables
	PermissionRewrite int64 // note meta rewrites at least this big; 0 disables
	PackageCache      bool  // note package manager caches
	Overwrite         int64 // note files replaced from earlier layers totalling at least this; 0 disables
}

// DefaultOptions are the built-in note thresholds.
func DefaultOptions() Options {
	return Options{LargeFile: 50_000_000, PermissionRewrite: 1_000_000, PackageCache: true, Overwrite: 10_000_000}
}

// Analyze replays img's layers.
func Analyze(img *index.Image, o Options) *Report {
	r := &Report{Ref: img.Ref, Source: string(img.Source), Platform: img.Platform, Digest: img.Digest, Compressed: img.Compressed}
	steps, created, before, after := pairSteps(img.History, len(img.Layers))
	r.EmptyAfter = after
	fs := newFS()
	pk := packageTracker{sources: map[string]pkgSource{}, prev: pkgdb.Set{}}
	for i, il := range img.Layers {
		l := Layer{Index: i, DiffID: il.DiffID, Digest: il.Digest, Size: il.Size, Compressed: il.Compressed, Step: steps[i], Created: created[i], EmptyBefore: before[i]}
		var waste []WasteItem
		for j := range il.Entries {
			l.Changes, waste = fs.apply(&il.Entries[j], i, l.Changes, waste)
			pk.apply(&il.Entries[j], i, il.Packages)
		}
		l.Packages = pk.diff()
		for _, c := range l.Changes {
			switch c.Type {
			case Added:
				l.Counts.Added++
			case Modified:
				l.Counts.Modified++
			case Meta:
				l.Counts.Meta++
			case Deleted:
				l.Counts.Deleted++
			}
		}
		r.WasteItems = append(r.WasteItems, waste...)
		r.Size += il.Size
		r.Layers = append(r.Layers, l)
	}
	for _, w := range r.WasteItems {
		r.Layers[w.StoredIn].Wasted += w.Size
		r.Waste += w.Size
	}
	sort.SliceStable(r.WasteItems, func(i, j int) bool {
		if r.WasteItems[i].Size != r.WasteItems[j].Size {
			return r.WasteItems[i].Size > r.WasteItems[j].Size
		}
		return r.WasteItems[i].Path < r.WasteItems[j].Path
	})
	for i := range r.Layers {
		l := &r.Layers[i]
		l.Mostly = mostly(l.Changes)
		l.Summary = summarize(l.Mostly)
		if l.Packages != nil {
			l.Summary = l.Packages.Summary()
		}
		l.Notes = notes(l.Changes, l.Step, o)
	}
	r.Final = fs.final()
	r.Packages = pk.prev
	return r
}

// pkgSource is the packages one database file (or .dist-info) declares,
// and the layer that stored it.
type pkgSource struct {
	pkgs  []pkgdb.Pkg
	layer int
}

// packageTracker follows package database files through the layers the way
// the filesystem does, so each layer's package changes can be diffed.
type packageTracker struct {
	sources map[string]pkgSource
	prev    pkgdb.Set
}

func (t *packageTracker) apply(e *index.Entry, layer int, declared map[string][]pkgdb.Pkg) {
	switch e.Type {
	case index.Whiteout:
		for p := range t.sources {
			if p == e.Path || strings.HasPrefix(p, e.Path+"/") {
				delete(t.sources, p)
			}
		}
	case index.Opaque:
		for p, s := range t.sources {
			if strings.HasPrefix(p, e.Path+"/") && s.layer < layer {
				delete(t.sources, p)
			}
		}
	default:
		if ps, ok := declared[e.Path]; ok {
			t.sources[e.Path] = pkgSource{pkgs: ps, layer: layer}
		} else {
			delete(t.sources, e.Path) // replaced by something that declares nothing
		}
	}
}

// diff returns the change since the last call, or nil if there was none.
func (t *packageTracker) diff() *pkgdb.Diff {
	paths := make([]string, 0, len(t.sources))
	for p := range t.sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	cur := pkgdb.Set{}
	for _, p := range paths {
		cur.Add(t.sources[p].pkgs...)
	}
	d := pkgdb.Compare(t.prev, cur)
	t.prev = cur
	if d.Empty() {
		return nil
	}
	return &d
}

// pairSteps pairs history entries that produced layers with the layers,
// aligned at the end (some builders drop the base image's history), and
// gathers the empty steps (ENV, LABEL, ...) between them.
func pairSteps(hist []index.Step, n int) (steps []string, created []time.Time, before [][]string, after []string) {
	steps, created, before = make([]string, n), make([]time.Time, n), make([][]string, n)
	var real []int
	for i, h := range hist {
		if !h.Empty {
			real = append(real, i)
		}
	}
	offset := n - len(real)
	prev := -1
	for k, hi := range real {
		layer := k + offset
		if layer >= 0 {
			steps[layer], created[layer] = CleanStep(hist[hi].CreatedBy), hist[hi].Created
			for _, h := range hist[prev+1 : hi] {
				before[layer] = append(before[layer], CleanStep(h.CreatedBy))
			}
		}
		prev = hi
	}
	for _, h := range hist[prev+1:] {
		after = append(after, CleanStep(h.CreatedBy))
	}
	return steps, created, before, after
}

// CleanStep makes a history entry read like the Dockerfile line it came
// from: "/bin/sh -c #(nop)  ENV A=1" is "ENV A=1", and BuildKit's
// "RUN |2 A=1 B=2 /bin/sh -c make # buildkit" is "RUN make".
func CleanStep(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSuffix(s, " # buildkit")
	if rest, ok := strings.CutPrefix(s, "/bin/sh -c #(nop) "); ok {
		return rest
	}
	if rest, ok := strings.CutPrefix(s, "/bin/sh -c "); ok {
		return "RUN " + rest
	}
	if rest, ok := strings.CutPrefix(s, "RUN |"); ok {
		// Drop the build-arg count and the NAME=value pairs that follow it.
		fields := strings.Fields(rest)
		i := 1
		for i < len(fields) && strings.Contains(fields[i], "=") && !strings.HasPrefix(fields[i], "/") {
			i++
		}
		s = "RUN " + strings.Join(fields[i:], " ")
	}
	if rest, ok := strings.CutPrefix(s, "RUN /bin/sh -c "); ok {
		return "RUN " + rest
	}
	return s
}

// mostly finds where a layer's bytes went: descend from / while a single
// directory holds at least 80% of the bytes. If no directory dominates the
// root, it returns the top three directories instead.
func mostly(changes []Change) []DirBytes {
	root := &sizeNode{}
	for _, c := range changes {
		if c.Type == Deleted || c.Size == 0 {
			continue
		}
		root.add(c.Path, c.Size)
	}
	if root.bytes == 0 {
		return nil
	}
	cur, curPath := root, "/"
	for {
		name, big := cur.biggest()
		if big == nil || len(big.kids) == 0 || big.bytes*5 < cur.bytes*4 {
			break
		}
		cur, curPath = big, path.Join(curPath, name)
	}
	if cur != root {
		return []DirBytes{{Path: curPath, Bytes: cur.bytes}}
	}
	var out []DirBytes
	for name, k := range root.kids {
		if len(k.kids) > 0 && k.bytes*10 >= root.bytes {
			out = append(out, DirBytes{Path: "/" + name, Bytes: k.bytes})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

type sizeNode struct {
	bytes int64
	kids  map[string]*sizeNode
}

func (n *sizeNode) add(p string, size int64) {
	n.bytes += size
	for _, part := range strings.Split(strings.TrimPrefix(p, "/"), "/") {
		if n.kids == nil {
			n.kids = map[string]*sizeNode{}
		}
		k := n.kids[part]
		if k == nil {
			k = &sizeNode{}
			n.kids[part] = k
		}
		k.bytes += size
		n = k
	}
}

func (n *sizeNode) biggest() (string, *sizeNode) {
	var (
		name string
		big  *sizeNode
	)
	for k, v := range n.kids {
		if big == nil || v.bytes > big.bytes || (v.bytes == big.bytes && k < name) {
			name, big = k, v
		}
	}
	return name, big
}
