package pkgdb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"
)

// stanzaDBs are the stanza-format databases Parse reads, with the manager
// each one reports.
var stanzaDBs = []struct{ path, manager string }{
	{"/var/lib/dpkg/status", "dpkg"},
	{"/var/lib/dpkg/status.d/base-files", "dpkg"},
	{"/lib/apk/db/installed", "apk"},
}

// FuzzParse feeds damaged dpkg and apk databases to Parse: layers are
// untrusted, so a crafted file may yield nothing but must never crash, and
// every package it yields has a trimmed name found in the file and the
// database's manager.
func FuzzParse(f *testing.F) {
	apk := "C:Q1abc\nP:musl\nV:1.2.5-r0\n\nP:busybox\nV:1.36.1-r29\n"
	for i, seed := range []string{dpkgStatus, "Package: base-files\nVersion: 12.4\n", apk, "", "\n\n", "Package:\nPackage: \t\n", "P: x\r\nV:1\r\n"} {
		f.Add(uint8(i%len(stanzaDBs)), []byte(seed))
	}
	f.Add(uint8(2), []byte(apk))
	f.Fuzz(func(t *testing.T, which uint8, data []byte) {
		db := stanzaDBs[int(which)%len(stanzaDBs)]
		if !IsDB(db.path) {
			t.Fatalf("IsDB(%s) = false", db.path)
		}
		ps := Parse(db.path, data)
		for _, p := range ps {
			if p.Name == "" || p.Name != strings.TrimSpace(p.Name) || p.Version != strings.TrimSpace(p.Version) {
				t.Fatalf("%s: package %+v has an empty or untrimmed field", db.path, p)
			}
			if p.Manager != db.manager {
				t.Fatalf("%s: manager %q, want %q", db.path, p.Manager, db.manager)
			}
			if !bytes.Contains(data, []byte(p.Name)) {
				t.Fatalf("%s: name %q is not in the file", db.path, p.Name)
			}
		}
		if n := bytes.Count(data, []byte("\n")) + 1; len(ps) > n {
			t.Fatalf("%s: %d packages from %d lines", db.path, len(ps), n)
		}
	})
}

// dpkgDB is a dpkg status file with n installed packages, each with a
// multi-line description, like a Debian image's.
func dpkgDB(n int) []byte {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "Package: pkg%d\nStatus: install ok installed\nPriority: optional\nVersion: 1.%d-1\nDepends: libc6 (>= 2.34)\nDescription: package %d\n a longer description: with colons\n .\n and more lines\n\n", i, i, i)
	}
	return []byte(b.String())
}

// BenchmarkParseDpkg measures reading a 400-package dpkg status file.
func BenchmarkParseDpkg(b *testing.B) {
	data := dpkgDB(400)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if got := Parse("/var/lib/dpkg/status", data); len(got) != 400 {
			b.Fatalf("%d packages", len(got))
		}
	}
}

// BenchmarkParseAPK measures reading a 200-package apk database.
func BenchmarkParseAPK(b *testing.B) {
	var s strings.Builder
	for i := range 200 {
		fmt.Fprintf(&s, "C:Q1abc%d=\nP:pkg%d\nV:1.%d-r0\nA:x86_64\nS:1234\nI:5678\nT:a package\nF:usr/bin\nR:pkg%d\n\n", i, i, i, i)
	}
	data := []byte(s.String())
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if got := Parse("/lib/apk/db/installed", data); len(got) != 200 {
			b.Fatalf("%d packages", len(got))
		}
	}
}

// BenchmarkParseRPM measures reading each kind of rpm database: the sqlite
// fixture, and Berkeley DB and ndb files built around header blobs.
func BenchmarkParseRPM(b *testing.B) {
	sqlite, err := os.ReadFile("testdata/rpmdb.sqlite")
	if err != nil {
		b.Fatal(err)
	}
	blob := rpmBlob("bash", "5.2", "1", 1, 10)
	for _, bc := range []struct {
		name string
		data []byte
	}{
		{"sqlite", sqlite},
		{"bdb", bdbFile(binary.LittleEndian, blob, rpmBlob("glibc", "2.39", "2", -1, 700))},
		{"ndb", ndbFile(blob, rpmBlob("glibc", "2.39", "2", -1, 700))},
	} {
		b.Run(bc.name, func(b *testing.B) {
			if len(parseRPM(bc.data)) == 0 {
				b.Fatal("no packages")
			}
			b.SetBytes(int64(len(bc.data)))
			b.ReportAllocs()
			for b.Loop() {
				parseRPM(bc.data)
			}
		})
	}
}
