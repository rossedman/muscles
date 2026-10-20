package pkgdb

import (
	"strings"
	"testing"
)

const dpkgStatus = `Package: curl
Status: install ok installed
Version: 7.88.1-10
Description: a tool
 with a continuation line: that has a colon

Package: wget
Status: deinstall ok config-files
Version: 1.21

Package: libc6
Status: install ok installed
Version: 2.36-9
`

func TestParse(t *testing.T) {
	ps := Parse("/var/lib/dpkg/status", []byte(dpkgStatus))
	if len(ps) != 2 || ps[0] != (Pkg{"dpkg", "curl", "7.88.1-10"}) || ps[1].Name != "libc6" {
		t.Fatalf("dpkg: %+v", ps)
	}
	apk := Parse("/lib/apk/db/installed", []byte("C:Q1abc\nP:musl\nV:1.2.5-r0\n\nP:busybox\nV:1.36.1-r29\n"))
	if len(apk) != 2 || apk[0] != (Pkg{"apk", "musl", "1.2.5-r0"}) {
		t.Fatalf("apk: %+v", apk)
	}
	// distroless: one stanza per file, no Status field.
	one := Parse("/var/lib/dpkg/status.d/base-files", []byte("Package: base-files\nVersion: 12.4\n"))
	if len(one) != 1 || one[0].Version != "12.4" {
		t.Fatalf("status.d: %+v", one)
	}
	for p, want := range map[string]bool{
		"/var/lib/dpkg/status":                  true,
		"/lib/apk/db/installed":                 true,
		"/var/lib/dpkg/status.d/tzdata":         true,
		"/var/lib/dpkg/status.d/tzdata.md5sums": false,
		"/var/lib/dpkg/status-old":              false,
		"/usr/share/doc/curl/copyright":         false,
	} {
		if IsDB(p) != want {
			t.Errorf("IsDB(%s) != %v", p, want)
		}
	}
}

func TestFromPath(t *testing.T) {
	p, ok := FromPath("/usr/local/lib/python3.12/site-packages/Flask_Login-0.6.3.dist-info/METADATA")
	if !ok || p != (Pkg{"pip", "flask-login", "0.6.3"}) {
		t.Fatalf("FromPath: %+v %v", p, ok)
	}
	if _, ok := FromPath("/usr/lib/python3/dist-packages/six-1.16.0.dist-info/RECORD"); ok {
		t.Fatal("RECORD matched")
	}
	if p, ok := FromPath("/usr/lib/python3/dist-packages/six-1.16.0.dist-info/METADATA"); !ok || p.Name != "six" {
		t.Fatal("dist-packages")
	}
}

func TestCompareAndSummary(t *testing.T) {
	a, b := Set{}, Set{}
	a.Add(Pkg{"dpkg", "libc6", "2.36-9"}, Pkg{"dpkg", "wget", "1.21"}, Pkg{"dpkg", "bash", "5.2"})
	b.Add(Pkg{"dpkg", "libc6", "2.36-9+deb12u3"}, Pkg{"dpkg", "curl", "7.88"}, Pkg{"dpkg", "git", "2.39"}, Pkg{"dpkg", "bash", "5.2"})
	d := Compare(a, b)
	if len(d.Installed) != 2 || len(d.Removed) != 1 || len(d.Upgraded) != 1 || d.Empty() {
		t.Fatalf("diff: %+v", d)
	}
	if got := d.Summary(); got != "installed curl 7.88 and git 2.39 (dpkg); upgraded libc6 2.36-9 → 2.36-9+deb12u3; removed wget 1.21 (dpkg)" {
		t.Fatalf("summary: %q", got)
	}
	if !Compare(a, a).Empty() || Compare(a, a).Summary() != "" {
		t.Fatal("no change")
	}

	many := Set{}
	for _, n := range strings.Split("a b c d e", " ") {
		many.Add(Pkg{"apk", n, "1"})
	}
	if got := Compare(Set{}, many).Summary(); got != "installed a, b and 3 more (apk)" {
		t.Fatalf("many: %q", got)
	}
	for _, n := range strings.Split("f g h i j k l m", " ") {
		many.Add(Pkg{"apk", n, "1"})
	}
	if got := Compare(Set{}, many).Summary(); got != "installed 13 packages (apk)" {
		t.Fatalf("lots: %q", got)
	}
	up := Set{}
	for k, p := range many {
		p.Version = "2"
		up[k] = p
	}
	if got := Compare(many, up).Summary(); got != "upgraded 13 packages" {
		t.Fatalf("upgrades: %q", got)
	}
	mixed := Set{}
	mixed.Add(Pkg{"pip", "pandas", "2.2"})
	mixed.Add(Pkg{"dpkg", "curl", "7"})
	if got := Compare(Set{}, mixed).Summary(); got != "installed curl 7 and pandas 2.2" {
		t.Fatalf("mixed: %q", got)
	}
	single := Set{}
	single.Add(Pkg{"pip", "six", "1.16"})
	if got := Compare(Set{}, single).Summary(); got != "installed six 1.16 (pip)" {
		t.Fatalf("single: %q", got)
	}
}
