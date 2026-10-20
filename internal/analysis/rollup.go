package analysis

import (
	"path"
	"sort"
	"strconv"
)

// Row is a change, or a directory of changes rolled up into one line.
type Row struct {
	Path      string     `json:"path"`
	Dir       bool       `json:"dir"` // a rolled-up directory: Count changes below Path
	Type      ChangeType `json:"type"`
	Kind      string     `json:"kind"`
	Size      int64      `json:"size"`
	PrevSize  int64      `json:"prev_size"`
	PrevLayer int        `json:"prev_layer"`
	Count     int        `json:"count"`
}

// WasteRow is a waste item, or a directory of them rolled up.
type WasteRow struct {
	Path     string     `json:"path"`
	Dir      bool       `json:"dir"`
	Size     int64      `json:"size"`
	Count    int        `json:"count"`
	StoredIn int        `json:"stored_in"`
	HiddenBy int        `json:"hidden_by"`
	Reason   ChangeType `json:"reason"`
}

// DefaultLimit is how many rows a rollup aims for.
const DefaultLimit = 30

// RollupChanges groups changes into at most about limit rows. A directory
// becomes one row only when everything below it changed the same way (same
// type, same previous layer); the biggest directories are opened first
// while rows remain. limit <= 0 lists every change.
func RollupChanges(changes []Change, limit int) []Row {
	items := make([]item, len(changes))
	for i, c := range changes {
		items[i] = item{path: c.Path, size: c.Size, prev: c.PrevSize, key: string(c.Type) + "|" + strconv.Itoa(c.PrevLayer), idx: i}
	}
	var rows []Row
	for _, g := range rollup(items, limit) {
		c := changes[g.idx]
		r := Row{Path: c.Path, Type: c.Type, Kind: c.Kind, Size: c.Size, PrevSize: c.PrevSize, PrevLayer: c.PrevLayer, Count: 1}
		if g.dir {
			r.Path, r.Dir, r.Kind, r.Size, r.PrevSize, r.Count = g.path, true, "dir", g.size, g.prev, g.count
		}
		rows = append(rows, r)
	}
	return rows
}

// RollupWaste groups waste items the same way, by reason and by the layers
// that stored and hid them.
func RollupWaste(ws []WasteItem, limit int) []WasteRow {
	items := make([]item, len(ws))
	for i, w := range ws {
		items[i] = item{path: w.Path, size: w.Size, key: string(w.Reason) + "|" + strconv.Itoa(w.StoredIn) + "|" + strconv.Itoa(w.HiddenBy), idx: i}
	}
	var rows []WasteRow
	for _, g := range rollup(items, limit) {
		w := ws[g.idx]
		r := WasteRow{Path: w.Path, Size: w.Size, Count: 1, StoredIn: w.StoredIn, HiddenBy: w.HiddenBy, Reason: w.Reason}
		if g.dir {
			r.Path, r.Dir, r.Size, r.Count = g.path, true, g.size, g.count
		}
		rows = append(rows, r)
	}
	return rows
}

type item struct {
	path       string
	size, prev int64
	key        string
	idx        int
}

type group struct {
	path       string
	dir        bool
	size, prev int64
	count      int
	idx        int // the first item in the group, for its shared fields
}

// rnode is a leaf (one item) or a directory (kids), never both. A path that
// appears twice, or a deleted directory with new files below it, keeps each
// item as a kid named "\x00<n>" at the directory's own path.
type rnode struct {
	path       string
	kids       map[string]*rnode
	leaf       *item
	size, prev int64
	count      int
	key        string
	uniform    bool
	first      int
}

func (n *rnode) dir() bool { return n.leaf == nil }

func (n *rnode) child(name, p string) *rnode {
	if n.leaf != nil {
		n.kids = map[string]*rnode{"\x00" + strconv.Itoa(n.leaf.idx): {path: n.path, leaf: n.leaf}}
		n.leaf = nil
	}
	if n.kids == nil {
		n.kids = map[string]*rnode{}
	}
	k := n.kids[name]
	if k == nil {
		k = &rnode{path: p}
		n.kids[name] = k
	}
	return k
}

func rollup(items []item, limit int) []group {
	root := &rnode{path: "/"}
	for i := range items {
		n := root
		if items[i].path != "/" {
			for _, part := range parts(items[i].path) {
				n = n.child(part, path.Join(n.path, part))
			}
		}
		if n.leaf == nil && len(n.kids) == 0 && n != root {
			n.leaf = &items[i]
		} else {
			n.child("\x00"+strconv.Itoa(i), n.path).leaf = &items[i]
		}
	}
	if len(items) == 0 {
		return nil
	}
	root.aggregate()

	// Directories that mix kinds of change must be opened; then open the
	// biggest directories while the rows fit. A directory with one entry is
	// always opened, since that costs no row.
	var frontier []*rnode
	var push func(n *rnode)
	push = func(n *rnode) {
		if n.dir() && (!n.uniform || limit <= 0) {
			for _, k := range n.kids {
				push(k)
			}
			return
		}
		frontier = append(frontier, n)
	}
	push(root)
	for {
		sortNodes(frontier)
		opened := false
		for i, n := range frontier {
			if n.dir() && (len(n.kids) == 1 || len(frontier)-1+len(n.kids) <= limit) {
				rest := append([]*rnode{}, frontier[i+1:]...)
				frontier = frontier[:i]
				for _, k := range n.kids {
					frontier = append(frontier, k)
				}
				frontier = append(frontier, rest...)
				opened = true
				break
			}
		}
		if !opened {
			break
		}
	}
	sortNodes(frontier)
	out := make([]group, len(frontier))
	for i, n := range frontier {
		if n.dir() {
			out[i] = group{path: n.path, dir: true, size: n.size, prev: n.prev, count: n.count, idx: n.first}
		} else {
			out[i] = group{path: n.leaf.path, size: n.leaf.size, prev: n.leaf.prev, count: 1, idx: n.leaf.idx}
		}
	}
	return out
}

func sortNodes(ns []*rnode) {
	sort.SliceStable(ns, func(i, j int) bool {
		if ns[i].size != ns[j].size {
			return ns[i].size > ns[j].size
		}
		if ns[i].path != ns[j].path {
			return ns[i].path < ns[j].path
		}
		return ns[i].first < ns[j].first
	})
}

func (n *rnode) aggregate() {
	n.uniform, n.first = true, -1
	if n.leaf != nil {
		n.size, n.prev, n.count, n.key, n.first = n.leaf.size, n.leaf.prev, 1, n.leaf.key, n.leaf.idx
		return
	}
	keyed := false
	for _, k := range n.kids {
		k.aggregate()
		n.size += k.size
		n.prev += k.prev
		n.count += k.count
		if !k.uniform || (keyed && k.key != n.key) {
			n.uniform = false
		}
		if !keyed {
			n.key, keyed = k.key, true
		}
		if n.first < 0 || k.first < n.first {
			n.first = k.first
		}
	}
}
