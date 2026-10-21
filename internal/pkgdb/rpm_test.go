package pkgdb

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

// rpmBlob builds an rpm header blob with a name, version, release and
// optional epoch, padded with a filler string tag.
func rpmBlob(name, version, release string, epoch, pad int) []byte {
	type entry struct{ tag, typ, off uint32 }
	var es []entry
	var store []byte
	str := func(tag uint32, v string) {
		es = append(es, entry{tag, typeString, uint32(len(store))})
		store = append(append(store, v...), 0)
	}
	str(tagName, name)
	str(tagVersion, version)
	str(tagRelease, release)
	if epoch >= 0 {
		for len(store)%4 != 0 {
			store = append(store, 0)
		}
		es = append(es, entry{tagEpoch, typeInt32, uint32(len(store))})
		store = binary.BigEndian.AppendUint32(store, uint32(epoch))
	}
	if pad > 0 {
		str(1004, strings.Repeat("x", pad))
	}
	b := binary.BigEndian.AppendUint32(nil, uint32(len(es)))
	b = binary.BigEndian.AppendUint32(b, uint32(len(store)))
	for _, e := range es {
		b = binary.BigEndian.AppendUint32(b, e.tag)
		b = binary.BigEndian.AppendUint32(b, e.typ)
		b = binary.BigEndian.AppendUint32(b, e.off)
		b = binary.BigEndian.AppendUint32(b, 1)
	}
	return append(b, store...)
}

func names(ps []Pkg) string {
	var s []string
	for _, p := range ps {
		s = append(s, p.Name+" "+p.Version)
	}
	return strings.Join(s, ", ")
}

func TestRPMHeader(t *testing.T) {
	if p, ok := rpmHeader(rpmBlob("openssl-libs", "3.2.6", "2.fc41", 1, 0)); !ok || p != (Pkg{"rpm", "openssl-libs", "1:3.2.6-2.fc41"}) {
		t.Errorf("epoch: %+v", p)
	}
	if p, _ := rpmHeader(rpmBlob("bash", "5.2", "", 0, 0)); p.Version != "5.2" {
		t.Errorf("no release, epoch 0: %+v", p)
	}
	for _, b := range [][]byte{nil, {0, 0, 0, 0, 0, 0, 0, 0}, rpmBlob("gpg-pubkey", "a", "b", -1, 0), rpmBlob("", "1", "1", -1, 0), {0, 0, 0, 9, 0, 0, 0, 0}} {
		if p, ok := rpmHeader(b); ok {
			t.Errorf("accepted %v: %+v", b, p)
		}
	}
	// An offset past the data store reads as empty.
	b := rpmBlob("bash", "5.2", "1", -1, 0)
	binary.BigEndian.PutUint32(b[8+16+8:], 1<<20)
	if p, _ := rpmHeader(b); p.Version != "-1" {
		t.Errorf("bad offset: %+v", p)
	}
}

// The fixture is a real SQLite database (written by Python's sqlite3, see
// testdata/README.md) with 512-byte pages, so its table has interior pages
// and its largest header spills onto overflow pages.
func TestRPMSQLite(t *testing.T) {
	data, err := os.ReadFile("testdata/rpmdb.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	ps := Parse("/var/lib/rpm/rpmdb.sqlite", data)
	if len(ps) != 43 || ps[0] != (Pkg{"rpm", "bash", "5.2.32-1.fc41"}) || ps[1].Version != "1:3.2.6-2.fc41" || ps[42].Name != "kernel-core" {
		t.Fatalf("%d packages: %s", len(ps), names(ps))
	}
	if !IsDB("/usr/lib/sysimage/rpm/rpmdb.sqlite") || IsDB("/var/lib/rpm/rpmdb.sqlite-wal") {
		t.Error("IsDB")
	}
	// No such table; a page size that isn't one; a 65536 page size marker.
	if sqliteBlobs(data, "Nope") != nil {
		t.Error("missing table")
	}
	bad := append([]byte{}, data...)
	binary.BigEndian.PutUint16(bad[16:], 7)
	if sqliteBlobs(bad, "Packages") != nil {
		t.Error("bad page size")
	}
	binary.BigEndian.PutUint16(bad[16:], 1)
	if sqliteBlobs(bad, "Packages") != nil {
		t.Error("65536-byte pages in a small file")
	}
	if sqliteBlobs(data[:50], "Packages") != nil {
		t.Error("short file")
	}
}

func TestVarint(t *testing.T) {
	for _, c := range []struct {
		b    []byte
		v    uint64
		n    int
		name string
	}{
		{[]byte{0x05}, 5, 1, "one byte"},
		{[]byte{0x81, 0x00}, 128, 2, "two bytes"},
		{bytes.Repeat([]byte{0xff}, 9), 1<<64 - 1, 9, "nine bytes"},
		{[]byte{0x81}, 0, 0, "short"},
	} {
		if v, n := varint(c.b); v != c.v || n != c.n {
			t.Errorf("%s: %d %d", c.name, v, n)
		}
	}
	if serialSize(5) != 6 || serialSize(7) != 8 || serialSize(9) != 0 || serialSize(13) != 0 || serialSize(1<<50) != -1 {
		t.Error("serialSize")
	}
	if sqliteInt(nil, 8) != 0 || sqliteInt(nil, 9) != 1 || sqliteInt([]byte{1, 0}, 2) != 256 {
		t.Error("sqliteInt")
	}
}

// bdbFile builds a Berkeley DB hash file: a metadata page, a hash page
// holding a key 0 record, an inline value and an off-page value that spans
// two overflow pages, and a page of another type that is skipped.
func bdbFile(bo binary.ByteOrder, inline, big []byte) []byte {
	const ps = 512
	data := make([]byte, 6*ps)
	bo.PutUint32(data[12:], bdbHashMagic)
	bo.PutUint32(data[20:], ps)

	h := data[ps : 2*ps]
	h[25] = bdbHashPage
	items := [][]byte{
		{bdbKeyData, 0, 0, 0, 0}, {bdbKeyData, 1, 0, 0, 0}, // key 0: rpm's counter, not a header
		{bdbKeyData, 2, 0, 0, 0}, append([]byte{bdbKeyData}, inline...),
		{bdbKeyData, 3, 0, 0, 0}, make([]byte, 12),
	}
	items[5][0] = bdbOffPage
	bo.PutUint32(items[5][4:], 3) // the first overflow page
	bo.PutUint32(items[5][8:], uint32(len(big)))
	bo.PutUint16(h[20:], uint16(len(items)))
	end := ps
	for i, it := range items {
		end -= len(it)
		copy(h[end:], it)
		bo.PutUint16(h[bdbPageHeader+2*i:], uint16(end))
	}

	room := ps - bdbPageHeader
	for i, n := range []int{3, 4} {
		o := data[n*ps : (n+1)*ps]
		o[25] = bdbOverflow
		chunk := big[i*room : min((i+1)*room, len(big))]
		copy(o[bdbPageHeader:], chunk)
		bo.PutUint16(o[22:], uint16(len(chunk)))
		if i == 0 {
			bo.PutUint32(o[16:], 4)
		}
	}
	data[5*ps+25] = 9 // some other page type
	return data
}

func TestRPMBerkeleyDB(t *testing.T) {
	inline, big := rpmBlob("bash", "4.2.46", "34.amzn2", -1, 0), rpmBlob("glibc", "2.26", "64.amzn2", -1, 700)
	for _, bo := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		ps := Parse("/var/lib/rpm/Packages", bdbFile(bo, inline, big))
		if names(ps) != "bash 4.2.46-34.amzn2, glibc 2.26-64.amzn2" {
			t.Errorf("%v: %s", bo, names(ps))
		}
	}
	// A broken overflow chain drops that value only.
	data := bdbFile(binary.LittleEndian, inline, big)
	data[4*512+25] = 0
	if ps := parseRPM(data); names(ps) != "bash 4.2.46-34.amzn2" {
		t.Errorf("broken chain: %s", names(ps))
	}
	bad := bdbFile(binary.LittleEndian, inline, big)
	binary.LittleEndian.PutUint32(bad[20:], 100)
	for _, d := range [][]byte{bad, make([]byte, 600), data[:40]} {
		if ps := bdbBlobs(d); ps != nil {
			t.Errorf("accepted %d bytes", len(d))
		}
	}
}

// ndbFile builds an ndb Packages.db with one slot page: two packages, an
// empty slot and a slot whose blob header doesn't match it.
func ndbFile(blobs ...[]byte) []byte {
	le := binary.LittleEndian
	data := make([]byte, ndbPage)
	le.PutUint32(data, ndbMagic)
	le.PutUint32(data[12:], 1)
	slot := ndbHeaderSize
	addSlot := func(pkg uint32, blob []byte, blobPkg uint32) {
		for len(data)%ndbBlobUnit != 0 {
			data = append(data, 0)
		}
		off := len(data)
		data = le.AppendUint32(data, ndbBlobMagic)
		data = le.AppendUint32(data, blobPkg)
		data = le.AppendUint32(data, 0)
		data = le.AppendUint32(data, uint32(len(blob)))
		data = append(data, blob...)
		le.PutUint32(data[slot:], ndbSlotMagic)
		le.PutUint32(data[slot+4:], pkg)
		le.PutUint32(data[slot+8:], uint32(off/ndbBlobUnit))
		slot += ndbSlotSize
	}
	for i, b := range blobs {
		addSlot(uint32(i+1), b, uint32(i+1))
	}
	addSlot(9, blobs[0], 8) // blob header names another package
	le.PutUint32(data[slot:], ndbSlotMagic)
	return data
}

func TestRPMNDB(t *testing.T) {
	data := ndbFile(rpmBlob("bash", "4.4", "150400.27.6.1", -1, 0), rpmBlob("libopenssl3", "3.1.4", "150600.5.50.1", -1, 0))
	if ps := Parse("/usr/lib/sysimage/rpm/Packages.db", data); names(ps) != "bash 4.4-150400.27.6.1, libopenssl3 3.1.4-150600.5.50.1" {
		t.Errorf("ndb: %s", names(ps))
	}
	if ndbBlobs(data[:10]) != nil {
		t.Error("short file")
	}
	// A slot pointing past the end, and a blob longer than the file.
	le := binary.LittleEndian
	cut := append([]byte{}, data...)
	le.PutUint32(cut[ndbHeaderSize+8:], 1<<20)
	b := append([]byte{}, data...)
	off := int(le.Uint32(b[ndbHeaderSize+ndbSlotSize+8:])) * ndbBlobUnit
	le.PutUint32(b[off+12:], 1<<20)
	if len(ndbBlobs(cut)) != 1 || len(ndbBlobs(b)) != 1 {
		t.Error("damaged slots")
	}
}

// FuzzParseRPM feeds damaged databases to the parsers: layers are untrusted,
// so a crafted file may yield nothing but must never crash muscles.
func FuzzParseRPM(f *testing.F) {
	sqlite, _ := os.ReadFile("testdata/rpmdb.sqlite")
	blob := rpmBlob("bash", "1", "1", 1, 10)
	for _, seed := range [][]byte{sqlite, bdbFile(binary.LittleEndian, blob, rpmBlob("glibc", "2", "2", -1, 700)), ndbFile(blob), blob} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		parseRPM(data)
		sqliteBlobs(data, "Packages")
		bdbBlobs(data)
		ndbBlobs(data)
		rpmHeader(data)
	})
}

// Every truncation and many single-byte corruptions of the fixtures parse
// without crashing, in the regular test run too.
func TestRPMDamaged(t *testing.T) {
	sqlite, _ := os.ReadFile("testdata/rpmdb.sqlite")
	blob := rpmBlob("bash", "1", "1", 1, 10)
	for _, d := range [][]byte{sqlite, bdbFile(binary.LittleEndian, blob, rpmBlob("glibc", "2", "2", -1, 700)), ndbFile(blob)} {
		for n := 0; n < len(d); n += 97 {
			parseRPM(d[:n])
			c := append([]byte{}, d...)
			c[n] ^= 0xff
			parseRPM(c)
			c[n] = 0x80
			parseRPM(c)
		}
	}
}
