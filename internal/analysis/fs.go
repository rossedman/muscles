package analysis

import (
	"path"
	"strings"

	"github.com/rossedman/muscles/internal/index"
)

// node is a path in the merged filesystem. e is nil for a directory no
// layer declared (its parent was implied by a deeper entry).
type node struct {
	kids  map[string]*node
	e     *index.Entry
	layer int
}

func (n *node) isDir() bool { return n.e == nil || n.e.Type == index.Dir }

func (n *node) kind() string {
	if n.e == nil {
		return index.Dir.String()
	}
	return n.e.Type.String()
}

type fsys struct{ root *node }

func newFS() *fsys { return &fsys{root: &node{kids: map[string]*node{}}} }

func parts(p string) []string { return strings.Split(strings.TrimPrefix(p, "/"), "/") }

// lookup returns the node at p, or nil.
func (f *fsys) lookup(p string) *node {
	n := f.root
	if p == "/" {
		return n
	}
	for _, part := range parts(p) {
		if n = n.kids[part]; n == nil {
			return nil
		}
	}
	return n
}

// parentOf returns the directory that holds p, creating implied
// directories (and replacing non-directories in the way) as needed.
func (f *fsys) parentOf(p string, layer int) *node {
	n := f.root
	ps := parts(p)
	for _, part := range ps[:len(ps)-1] {
		k := n.kids[part]
		if k == nil || !k.isDir() {
			k = &node{kids: map[string]*node{}, layer: layer}
			n.kids[part] = k
		}
		if k.kids == nil {
			k.kids = map[string]*node{}
		}
		n = k
	}
	return n
}

// remove takes n (at p) out of the filesystem view and returns the bytes it
// freed, recording each file's bytes as waste hidden by layer. With
// keepLayer, nodes stored by that layer survive (opaque whiteouts only hide
// lower layers); remove then reports whether anything was kept.
func remove(n *node, p string, layer int, reason ChangeType, keepLayer bool, waste []WasteItem) (int64, bool, []WasteItem) {
	if keepLayer && n.layer == layer && !n.isDir() {
		return 0, true, waste
	}
	var freed int64
	kept := false
	if n.isDir() {
		for name, k := range n.kids {
			f, keep, w := remove(k, path.Join(p, name), layer, reason, keepLayer, waste)
			freed, waste = freed+f, w
			if keep {
				kept = true
			} else {
				delete(n.kids, name)
			}
		}
		return freed, kept || (keepLayer && n.layer == layer), waste
	}
	if n.e.Size > 0 {
		waste = append(waste, WasteItem{Path: p, Size: n.e.Size, StoredIn: n.layer, HiddenBy: layer, Reason: reason})
	}
	return n.e.Size, false, waste
}

// apply replays one entry of layer and appends the change it makes.
func (f *fsys) apply(e *index.Entry, layer int, changes []Change, waste []WasteItem) ([]Change, []WasteItem) {
	switch e.Type {
	case index.Whiteout:
		dir, base := path.Split(e.Path)
		parent := f.lookup(path.Clean(dir))
		if parent == nil || parent.kids[base] == nil {
			return changes, waste // deleting something no lower layer has
		}
		n := parent.kids[base]
		freed, _, w := remove(n, e.Path, layer, Deleted, false, waste)
		delete(parent.kids, base)
		return append(changes, Change{Path: e.Path, Type: Deleted, Kind: n.kind(), Size: freed, PrevLayer: n.layer}), w
	case index.Opaque:
		d := f.lookup(e.Path)
		if d == nil || !d.isDir() {
			return changes, waste
		}
		before := len(waste)
		var freed int64
		for name, k := range d.kids {
			fr, keep, w := remove(k, path.Join(e.Path, name), layer, Deleted, true, waste)
			freed, waste = freed+fr, w
			if !keep {
				delete(d.kids, name)
			}
		}
		if freed == 0 && len(waste) == before {
			return changes, waste
		}
		return append(changes, Change{Path: e.Path, Type: Deleted, Kind: index.Dir.String(), Size: freed, PrevLayer: -1}), waste
	}

	parent := f.parentOf(e.Path, layer)
	base := path.Base(e.Path)
	old := parent.kids[base]
	if e.Type == index.Dir {
		switch {
		case old == nil:
			parent.kids[base] = &node{kids: map[string]*node{}, e: e, layer: layer}
		case old.isDir():
			old.e = e // a directory declared again; its contents stay
		default:
			freed, _, w := remove(old, e.Path, layer, Modified, false, waste)
			waste = w
			parent.kids[base] = &node{kids: map[string]*node{}, e: e, layer: layer}
			changes = append(changes, Change{Path: e.Path, Type: Modified, Kind: e.Type.String(), PrevSize: freed, PrevLayer: old.layer})
		}
		return changes, waste
	}

	c := Change{Path: e.Path, Kind: e.Type.String(), Size: e.Size, PrevLayer: -1}
	switch {
	case old == nil:
		c.Type = Added
	case old.isDir():
		freed, _, w := remove(old, e.Path, layer, Modified, false, waste)
		waste = w
		c.Type, c.PrevSize, c.PrevLayer = Modified, freed, old.layer
	default:
		same := old.e.Type == e.Type && old.e.Hash == e.Hash && old.e.Size == e.Size && old.e.Link == e.Link
		c.Type, c.PrevSize, c.PrevLayer = Modified, old.e.Size, old.layer
		if same {
			c.Type = Meta
		}
		if old.e.Size > 0 {
			waste = append(waste, WasteItem{Path: e.Path, Size: old.e.Size, StoredIn: old.layer, HiddenBy: layer, Reason: c.Type})
		}
	}
	parent.kids[base] = &node{e: e, layer: layer}
	return append(changes, c), waste
}

// final lists every path in the merged filesystem.
func (f *fsys) final() map[string]FinalEntry {
	out := map[string]FinalEntry{}
	var walk func(n *node, p string)
	walk = func(n *node, p string) {
		for name, k := range n.kids {
			kp := path.Join(p, name)
			e := index.Entry{Path: kp, Type: index.Dir}
			if k.e != nil {
				e = *k.e
			}
			out[kp] = FinalEntry{Entry: e, Layer: k.layer}
			walk(k, kp)
		}
	}
	walk(f.root, "/")
	return out
}
