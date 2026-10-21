package pkgdb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

// rpm keeps its installed packages in one of three databases, depending on
// the distribution and its age:
//
//	rpmdb.sqlite  SQLite: Fedora 33+, RHEL and UBI 9, Rocky and Alma 9, Amazon Linux 2023
//	Packages      Berkeley DB hash: RHEL and UBI 8 and older, CentOS 7, Amazon Linux 2
//	Packages.db   rpm's own "ndb" format: openSUSE and SLES 15
//
// Each stores one rpm header blob per package. Only the few structures
// needed to find those blobs are read; anything unexpected yields fewer
// packages, never an error.
var rpmDBs = map[string]bool{
	"/var/lib/rpm/rpmdb.sqlite":          true,
	"/usr/lib/sysimage/rpm/rpmdb.sqlite": true,
	"/var/lib/rpm/Packages":              true,
	"/usr/lib/sysimage/rpm/Packages":     true,
	"/var/lib/rpm/Packages.db":           true,
	"/usr/lib/sysimage/rpm/Packages.db":  true,
}

// parseRPM reads the packages in an rpm database of any of the three kinds.
func parseRPM(data []byte) []Pkg {
	var blobs [][]byte
	switch {
	case bytes.HasPrefix(data, []byte("SQLite format 3\x00")):
		blobs = sqliteBlobs(data, "Packages")
	case len(data) >= 4 && binary.LittleEndian.Uint32(data) == ndbMagic:
		blobs = ndbBlobs(data)
	default:
		blobs = bdbBlobs(data)
	}
	var out []Pkg
	for _, b := range blobs {
		if p, ok := rpmHeader(b); ok {
			out = append(out, p)
		}
	}
	return out
}

// rpm header tags and types.
const (
	tagName    = 1000
	tagVersion = 1001
	tagRelease = 1002
	tagEpoch   = 1003

	typeInt32  = 4
	typeString = 6
)

// rpmHeader reads a package's name and version from an rpm header blob: two
// big-endian counts (index entries, data bytes), the 16-byte index entries
// (tag, type, offset, count), then the data they point into.
func rpmHeader(b []byte) (Pkg, bool) {
	if len(b) < 8 {
		return Pkg{}, false
	}
	il, dl := int(binary.BigEndian.Uint32(b)), int(binary.BigEndian.Uint32(b[4:]))
	start := 8 + 16*il
	if il <= 0 || il > 1<<16 || dl < 0 || start+dl > len(b) {
		return Pkg{}, false
	}
	store := b[start : start+dl]
	str := func(off int) string {
		if off < 0 || off >= len(store) {
			return ""
		}
		s := store[off:]
		if i := bytes.IndexByte(s, 0); i >= 0 {
			s = s[:i]
		}
		return string(s)
	}
	var name, version, release string
	epoch := -1
	for i := 0; i < il; i++ {
		e := b[8+16*i:]
		tag, typ, off := binary.BigEndian.Uint32(e), binary.BigEndian.Uint32(e[4:]), int(binary.BigEndian.Uint32(e[8:]))
		switch {
		case tag == tagName && typ == typeString:
			name = str(off)
		case tag == tagVersion && typ == typeString:
			version = str(off)
		case tag == tagRelease && typ == typeString:
			release = str(off)
		case tag == tagEpoch && typ == typeInt32 && off >= 0 && off+4 <= len(store):
			epoch = int(binary.BigEndian.Uint32(store[off:]))
		}
	}
	// gpg-pubkey entries are imported signing keys, not packages.
	if name == "" || name == "gpg-pubkey" {
		return Pkg{}, false
	}
	v := version
	if release != "" {
		v += "-" + release
	}
	if epoch > 0 {
		v = fmt.Sprintf("%d:%s", epoch, v)
	}
	return Pkg{Manager: "rpm", Name: name, Version: v}, true
}

// sqliteBlobs returns the last column of every row of a table in an SQLite
// database file: the schema table (page 1) names the table's root page, and
// the table's b-tree is walked from there, following overflow chains.
func sqliteBlobs(data []byte, table string) [][]byte {
	if len(data) < 100 {
		return nil
	}
	pageSize := int(binary.BigEndian.Uint16(data[16:]))
	if pageSize == 1 {
		pageSize = 65536
	}
	if pageSize < 512 || pageSize > len(data) {
		return nil
	}
	db := &sqlite{data: data, pageSize: pageSize, usable: pageSize - int(data[20])}
	root := 0
	db.walk(1, func(rec [][]byte, types []uint64) {
		// sqlite_schema: type, name, tbl_name, rootpage, sql
		if len(rec) >= 4 && string(rec[0]) == "table" && strings.EqualFold(string(rec[1]), table) {
			root = int(sqliteInt(rec[3], types[3]))
		}
	})
	if root <= 0 {
		return nil
	}
	var out [][]byte
	db.walk(root, func(rec [][]byte, types []uint64) {
		if n := len(rec); n > 0 && types[n-1] >= 12 && types[n-1]%2 == 0 {
			out = append(out, rec[n-1])
		}
	})
	return out
}

type sqlite struct {
	data             []byte
	pageSize, usable int
	seen             map[int]bool
}

// page returns the 1-based database page n, or nil if it is out of range.
func (s *sqlite) page(n int) []byte {
	off := (n - 1) * s.pageSize
	if n < 1 || off+s.pageSize > len(s.data) {
		return nil
	}
	return s.data[off : off+s.pageSize]
}

// walk visits every row of the table b-tree rooted at page n.
func (s *sqlite) walk(n int, visit func(rec [][]byte, types []uint64)) {
	if s.seen == nil {
		s.seen = map[int]bool{}
	}
	if s.seen[n] {
		return // a cycle: the file is damaged
	}
	s.seen[n] = true
	p := s.page(n)
	if p == nil {
		return
	}
	h := 0
	if n == 1 {
		h = 100 // the database header comes first on page 1
	}
	if h+8 > len(p) {
		return
	}
	kind, cells := p[h], int(binary.BigEndian.Uint16(p[h+3:]))
	switch kind {
	case 0x05: // interior table page: child pointers, then the rightmost child
		if h+12 > len(p) {
			return
		}
		for i := 0; i < cells; i++ {
			if c := s.cell(p, h+12, i); c >= 0 && c+4 <= len(p) {
				s.walk(int(binary.BigEndian.Uint32(p[c:])), visit)
			}
		}
		s.walk(int(binary.BigEndian.Uint32(p[h+8:])), visit)
	case 0x0D: // leaf table page: records
		for i := 0; i < cells; i++ {
			c := s.cell(p, h+8, i)
			if c < 0 {
				continue
			}
			size, k := varint(p[c:])
			_, k2 := varint(p[c+k:])
			if k == 0 || k2 == 0 {
				continue
			}
			if payload := s.payload(p, c+k+k2, int(size)); payload != nil {
				if rec, types := record(payload); rec != nil {
					visit(rec, types)
				}
			}
		}
	}
}

// cell returns the offset of cell i, from the cell pointer array at ptrs.
func (s *sqlite) cell(p []byte, ptrs, i int) int {
	at := ptrs + 2*i
	if at+2 > len(p) {
		return -1
	}
	c := int(binary.BigEndian.Uint16(p[at:]))
	if c >= len(p) {
		return -1
	}
	return c
}

// payload assembles a leaf cell's payload of size bytes starting at off:
// what fits on the page, then the overflow pages it points to.
func (s *sqlite) payload(p []byte, off, size int) []byte {
	u := s.usable
	maxLocal := u - 35
	local := size
	if size > maxLocal {
		minLocal := (u-12)*32/255 - 23
		local = minLocal + (size-minLocal)%(u-4)
		if local > maxLocal {
			local = minLocal
		}
	}
	if size < 0 || off+local > len(p) {
		return nil
	}
	out := append([]byte{}, p[off:off+local]...)
	if local == size {
		return out
	}
	if off+local+4 > len(p) {
		return nil
	}
	next := int(binary.BigEndian.Uint32(p[off+local:]))
	for hops := 0; len(out) < size && next != 0 && hops < len(s.data)/s.pageSize; hops++ {
		op := s.page(next)
		if op == nil {
			return nil
		}
		take := min(size-len(out), u-4)
		out = append(out, op[4:4+take]...)
		next = int(binary.BigEndian.Uint32(op))
	}
	if len(out) != size {
		return nil
	}
	return out
}

// record splits an SQLite record into its column values and serial types.
func record(b []byte) ([][]byte, []uint64) {
	hsize, k := varint(b)
	if k == 0 || int(hsize) > len(b) {
		return nil, nil
	}
	var types []uint64
	for at := k; at < int(hsize); {
		t, n := varint(b[at:])
		if n == 0 {
			return nil, nil
		}
		types = append(types, t)
		at += n
	}
	rec := make([][]byte, len(types))
	at := int(hsize)
	for i, t := range types {
		n := serialSize(t)
		if n < 0 || at+n > len(b) {
			return nil, nil
		}
		rec[i] = b[at : at+n]
		at += n
	}
	return rec, types
}

// serialSize returns the byte length of a value whose SQLite record serial type is t.
func serialSize(t uint64) int {
	switch {
	case t <= 4:
		return int(t)
	case t == 5:
		return 6
	case t == 6, t == 7:
		return 8
	case t >= 12:
		if t > 1<<40 {
			return -1 // longer than any file muscles reads
		}
		return int((t - 12) / 2)
	}
	return 0 // 8 and 9 are the constants 0 and 1
}

// sqliteInt reads an integer column.
func sqliteInt(b []byte, t uint64) int64 {
	switch t {
	case 8:
		return 0
	case 9:
		return 1
	}
	var v int64
	for _, c := range b {
		v = v<<8 | int64(c)
	}
	return v
}

// varint reads an SQLite varint: up to eight 7-bit groups with a
// continuation bit, then a full ninth byte. It returns 0 bytes read when b
// is too short.
func varint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < 9 && i < len(b); i++ {
		if i == 8 {
			return v<<8 | uint64(b[i]), 9
		}
		v = v<<7 | uint64(b[i]&0x7f)
		if b[i] < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

// Berkeley DB hash database structures.
const (
	bdbHashMagic   = 0x061561
	bdbHashPage    = 13 // P_HASH
	bdbHashOldPage = 2  // P_HASH_UNSORTED
	bdbOverflow    = 7  // P_OVERFLOW
	bdbKeyData     = 1  // H_KEYDATA: an item stored on the page
	bdbOffPage     = 3  // H_OFFPAGE: an item stored on overflow pages
	bdbPageHeader  = 26
)

// bdbBlobs returns the values of a Berkeley DB hash database: rpm's
// Packages file, keyed by package number. Every hash page is read in turn;
// values on the page and on overflow chains are both returned.
func bdbBlobs(data []byte) [][]byte {
	if len(data) < 72 {
		return nil
	}
	var bo binary.ByteOrder = binary.LittleEndian
	if bo.Uint32(data[12:]) != bdbHashMagic {
		bo = binary.BigEndian
		if bo.Uint32(data[12:]) != bdbHashMagic {
			return nil
		}
	}
	pageSize := int(bo.Uint32(data[20:]))
	if pageSize < 512 || pageSize > 65536 {
		return nil
	}
	pages := len(data) / pageSize
	page := func(n int) []byte {
		if n <= 0 || n >= pages {
			return nil
		}
		return data[n*pageSize : (n+1)*pageSize]
	}
	var out [][]byte
	for n := 1; n < pages; n++ {
		p := page(n)
		if t := p[25]; t != bdbHashPage && t != bdbHashOldPage {
			continue
		}
		entries := int(bo.Uint16(p[20:]))
		offs := make([]int, 0, entries)
		for i := 0; i < entries; i++ {
			at := bdbPageHeader + 2*i
			if at+2 > pageSize {
				break
			}
			offs = append(offs, int(bo.Uint16(p[at:])))
		}
		// Entries alternate key, value; items are packed from the end of
		// the page down, so an item ends where the one before it starts.
		for i := 1; i < len(offs); i += 2 {
			off := offs[i]
			if off <= 0 || off >= pageSize {
				continue
			}
			switch p[off] {
			case bdbKeyData:
				end := pageSize
				if i > 0 && offs[i-1] > off {
					end = offs[i-1]
				}
				out = append(out, p[off+1:end])
			case bdbOffPage:
				if off+12 > pageSize {
					continue
				}
				next, size := int(bo.Uint32(p[off+4:])), int(bo.Uint32(p[off+8:]))
				var v []byte
				for hops := 0; next != 0 && len(v) < size && hops < pages; hops++ {
					op := page(next)
					if op == nil || op[25] != bdbOverflow {
						break
					}
					used := int(bo.Uint16(op[22:])) // hf_offset holds the bytes used
					if bdbPageHeader+used > pageSize {
						break
					}
					v = append(v, op[bdbPageHeader:bdbPageHeader+used]...)
					next = int(bo.Uint32(op[16:]))
				}
				if len(v) == size {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

// rpm's ndb structures, all little-endian.
const (
	ndbMagic      = 'R' | 'p'<<8 | 'm'<<16 | 'P'<<24
	ndbSlotMagic  = 'S' | 'l'<<8 | 'o'<<16 | 't'<<24
	ndbBlobMagic  = 'B' | 'l'<<8 | 'b'<<16 | 'S'<<24
	ndbPage       = 4096
	ndbHeaderSize = 32
	ndbSlotSize   = 16
	ndbBlobUnit   = 16
)

// ndbBlobs returns the header blobs of an ndb Packages.db: a 32-byte
// header, slot pages naming each package's blob (offset and length in
// 16-byte units), then the blobs, each behind a 16-byte blob header.
func ndbBlobs(data []byte) [][]byte {
	if len(data) < ndbHeaderSize {
		return nil
	}
	le := binary.LittleEndian
	slotPages := int(le.Uint32(data[12:]))
	end := min(slotPages*ndbPage, len(data))
	var out [][]byte
	for at := ndbHeaderSize; at+ndbSlotSize <= end; at += ndbSlotSize {
		s := data[at:]
		if le.Uint32(s) != ndbSlotMagic || le.Uint32(s[4:]) == 0 {
			continue
		}
		pkg, off := le.Uint32(s[4:]), int(le.Uint32(s[8:]))*ndbBlobUnit
		if off+16 > len(data) {
			continue
		}
		b := data[off:]
		if le.Uint32(b) != ndbBlobMagic || le.Uint32(b[4:]) != pkg {
			continue
		}
		size := int(le.Uint32(b[12:]))
		if 16+size > len(b) {
			continue
		}
		out = append(out, b[16:16+size])
	}
	return out
}
