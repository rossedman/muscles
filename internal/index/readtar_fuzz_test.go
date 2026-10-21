package index

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/rossedman/muscles/internal/pkgdb"
	"github.com/rossedman/muscles/internal/testimg"
)

// dpkgStatus is a small dpkg status file: one installed package, one removed.
const dpkgStatus = "Package: curl\nStatus: install ok installed\nVersion: 7.88.1-10\n\nPackage: wget\nStatus: deinstall ok config-files\nVersion: 1.21\n"

// tarSeeds are realistic layer tars: a base layer, an apt install with a
// dpkg status file, a layer that deletes with whiteouts and an opaque
// directory, links of both kinds, an apk database and Python metadata.
func tarSeeds() [][]byte {
	return [][]byte{
		testimg.TarBytes(),
		testimg.TarBytes(
			testimg.Dir("./"),
			testimg.Dir("./usr/"),
			testimg.File{Path: "./usr/bin/curl", Body: "curl", Mode: 0o4755, UID: 1, GID: 2},
			testimg.File{Path: "usr/bin/sh", Type: tar.TypeSymlink, Link: "dash"},
			testimg.File{Path: "usr/bin/ln", Type: tar.TypeLink, Link: "./usr/bin/curl"},
			testimg.File{Path: "dev/null", Type: tar.TypeChar},
		),
		testimg.TarBytes(
			testimg.Dir("var/lib/dpkg"),
			testimg.File{Path: "var/lib/dpkg/status", Body: dpkgStatus},
			testimg.File{Path: "var/lib/dpkg/status.d/base-files", Body: "Package: base-files\nVersion: 12.4\n"},
			testimg.File{Path: "var/lib/dpkg/status.d/base-files.md5sums", Body: "abc  etc/issue\n"},
		),
		testimg.TarBytes(
			testimg.Whiteout("etc/old.conf"),
			testimg.Opaque("var/cache"),
			testimg.File{Path: "var/.wh..wh.plnk"},
			testimg.File{Path: ".wh..wh..opq"},
			testimg.File{Path: "a/.wh."},
		),
		testimg.TarBytes(
			testimg.File{Path: "lib/apk/db/installed", Body: "C:Q1abc\nP:musl\nV:1.2.5-r0\n\nP:busybox\nV:1.36.1-r29\n"},
			testimg.File{Path: "usr/lib/python3/dist-packages/six-1.16.0.dist-info/METADATA", Body: "Name: six\n"},
			testimg.File{Path: "../../etc/passwd", Body: "root:x:0:0\n"},
			testimg.File{Path: "/abs//double/./dot/", Type: tar.TypeDir},
		),
	}
}

// FuzzReadTar feeds arbitrary bytes to the layer reader: layers are
// untrusted, so a crafted tar may be rejected but must never crash muscles,
// and whatever it accepts must keep the indexer's invariants.
func FuzzReadTar(f *testing.F) {
	for _, s := range tarSeeds() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		entries, total, pkgs, err := ReadTar(context.Background(), bytes.NewReader(data))
		if err != nil {
			if entries != nil || total != 0 || pkgs != nil {
				t.Fatalf("error %v with results %v %d %v", err, entries, total, pkgs)
			}
			return
		}
		checkEntries(t, entries, total, pkgs)
		// Indexing is deterministic: the cache relies on it.
		again, total2, pkgs2, err := ReadTar(context.Background(), bytes.NewReader(data))
		if err != nil || total2 != total || !reflect.DeepEqual(again, entries) || !reflect.DeepEqual(pkgs2, pkgs) {
			t.Fatalf("second read differs: %v", err)
		}
	})
}

// checkEntries asserts what ReadTar promises about an accepted layer.
func checkEntries(t *testing.T, entries []Entry, total int64, pkgs map[string][]pkgdb.Pkg) {
	t.Helper()
	var sum int64
	files := map[string]bool{}
	for i, e := range entries {
		if !strings.HasPrefix(e.Path, "/") || Clean(e.Path) != e.Path {
			t.Fatalf("entry %d path %q is not absolute and clean", i, e.Path)
		}
		if e.Path == "/" && e.Type != Opaque && e.Type != Whiteout {
			t.Fatalf("entry %d is the root: %+v", i, e)
		}
		if e.Type > Opaque {
			t.Fatalf("entry %d has type %d", i, e.Type)
		}
		if e.Mode&^0o7777 != 0 {
			t.Fatalf("entry %d mode %o has file type bits", i, e.Mode)
		}
		switch e.Type {
		case File:
			if e.Size < 0 {
				t.Fatalf("entry %d size %d", i, e.Size)
			}
			sum += e.Size
			files[e.Path] = true
		case Hardlink:
			if Clean(e.Link) != e.Link {
				t.Fatalf("hardlink %d target %q not clean", i, e.Link)
			}
		case Whiteout, Opaque:
			if e != (Entry{Path: e.Path, Type: e.Type}) {
				t.Fatalf("marker %d carries fields: %+v", i, e)
			}
		}
		if e.Type != File && (e.Size != 0 || e.Hash != 0) {
			t.Fatalf("non-file %d has size or hash: %+v", i, e)
		}
	}
	if total != sum {
		t.Fatalf("total %d, regular files sum to %d", total, sum)
	}
	for p, ps := range pkgs {
		if !files[p] {
			t.Fatalf("packages from %q, which is not a regular file in the layer", p)
		}
		for _, pkg := range ps {
			if pkg.Name == "" || pkg.Manager == "" {
				t.Fatalf("package without name or manager in %q: %+v", p, pkg)
			}
		}
	}
}

// TestReadTarSeeds checks the fuzz invariants on every seed, and that the
// seeds that hold package databases yield their packages.
func TestReadTarSeeds(t *testing.T) {
	t.Parallel()
	seeds := tarSeeds()
	for i, s := range seeds {
		entries, total, pkgs, err := ReadTar(t.Context(), bytes.NewReader(s))
		if err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
		checkEntries(t, entries, total, pkgs)
	}
	_, _, pkgs, _ := ReadTar(t.Context(), bytes.NewReader(seeds[2]))
	if len(pkgs["/var/lib/dpkg/status"]) != 1 || len(pkgs["/var/lib/dpkg/status.d/base-files"]) != 1 || len(pkgs) != 2 {
		t.Fatalf("dpkg packages: %+v", pkgs)
	}
	entries, _, pkgs, _ := ReadTar(t.Context(), bytes.NewReader(seeds[4]))
	if len(pkgs["/lib/apk/db/installed"]) != 2 || len(pkgs["/usr/lib/python3/dist-packages/six-1.16.0.dist-info/METADATA"]) != 1 || len(pkgs) != 2 {
		t.Fatalf("apk and pip packages: %+v", pkgs)
	}
	if entries[2].Path != "/etc/passwd" || entries[3].Path != "/abs/double/dot" {
		t.Fatalf("escaping and messy names: %+v", entries)
	}
	entries, _, _, _ = ReadTar(t.Context(), bytes.NewReader(seeds[3]))
	want := []Entry{{Path: "/etc/old.conf", Type: Whiteout}, {Path: "/var/cache", Type: Opaque}, {Path: "/", Type: Opaque}, {Path: "/a", Type: Whiteout}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("markers: %+v", entries)
	}
}

// benchLayer is a layer the size of a modest apt install: n files of mixed
// sizes in nested directories, links, a whiteout and a dpkg status file.
func benchLayer(n int) []byte {
	var status strings.Builder
	files := []testimg.File{testimg.Dir("usr"), testimg.Dir("usr/lib"), testimg.Dir("var/lib/dpkg")}
	for i := range n {
		dir := fmt.Sprintf("usr/lib/pkg%03d", i/50)
		if i%50 == 0 {
			files = append(files, testimg.Dir(dir))
			fmt.Fprintf(&status, "Package: pkg%03d\nStatus: install ok installed\nVersion: 1.%d\n\n", i/50, i)
		}
		files = append(files, testimg.File{Path: fmt.Sprintf("%s/file%04d.so", dir, i), Body: strings.Repeat(string(rune('a'+i%26)), 64+i%4096)})
	}
	files = append(files,
		testimg.File{Path: "usr/bin/sh", Type: tar.TypeSymlink, Link: "dash"},
		testimg.File{Path: "usr/bin/ln", Type: tar.TypeLink, Link: "usr/lib/pkg000/file0000.so"},
		testimg.Whiteout("var/cache/apt/pkgcache.bin"),
		testimg.File{Path: "var/lib/dpkg/status", Body: status.String()},
	)
	return testimg.TarBytes(files...)
}

// BenchmarkReadTar measures indexing one layer: tar parsing, hashing every
// file and parsing the package database.
func BenchmarkReadTar(b *testing.B) {
	layer := benchLayer(2000)
	b.SetBytes(int64(len(layer)))
	b.ReportAllocs()
	for b.Loop() {
		if _, _, _, err := ReadTar(b.Context(), bytes.NewReader(layer)); err != nil {
			b.Fatal(err)
		}
	}
}
