// Package pkgdb reads the package databases found in image layers (dpkg's
// status file and status.d entries, apk's installed file, rpm's databases)
// and Python .dist-info directories, and compares package sets. It does no I/O: the
// indexer hands it the bytes of the files IsDB names.
package pkgdb

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// MaxDB is the largest database file read; bigger ones are skipped. rpm
// databases of large images run to tens of megabytes.
const MaxDB = 128 << 20

// Pkg is one installed package.
type Pkg struct {
	Manager string `json:"manager"` // dpkg, apk, rpm or pip
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (p Pkg) key() string { return p.Manager + "/" + p.Name }

// IsDB reports whether the file at path is a package database whose
// contents Parse needs.
func IsDB(p string) bool {
	switch {
	case p == "/var/lib/dpkg/status", p == "/lib/apk/db/installed", rpmDBs[p]:
		return true
	case strings.HasPrefix(p, "/var/lib/dpkg/status.d/"):
		return !strings.HasSuffix(path.Base(p), ".md5sums") // skip the checksums beside each entry
	}
	return false
}

// Parse reads the packages in a database file.
func Parse(p string, data []byte) []Pkg {
	if p == "/lib/apk/db/installed" {
		return stanzas(data, "apk", "P", "V", "")
	}
	if rpmDBs[p] {
		return parseRPM(data)
	}
	return stanzas(data, "dpkg", "Package", "Version", "Status")
}

// stanzas reads blank-line separated "Key: value" records. A record whose
// status says it isn't installed is skipped.
func stanzas(data []byte, manager, nameKey, versionKey, statusKey string) []Pkg {
	var out []Pkg
	var cur Pkg
	installed := true
	flush := func() {
		if cur.Name != "" && installed {
			cur.Manager = manager
			out = append(out, cur)
		}
		cur, installed = Pkg{}, true
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case nameKey:
			cur.Name = v
		case versionKey:
			cur.Version = v
		case statusKey:
			installed = strings.HasSuffix(v, " installed")
		}
	}
	flush()
	return out
}

// distInfo matches a Python package's metadata file:
// .../site-packages/pandas-2.2.3.dist-info/METADATA.
var distInfo = regexp.MustCompile(`/(?:site|dist)-packages/([A-Za-z0-9_.]+)-([^/-]+)\.dist-info/METADATA$`)

// FromPath returns the Python package a file path declares, if it is a
// .dist-info METADATA file.
func FromPath(p string) (Pkg, bool) {
	m := distInfo.FindStringSubmatch(p)
	if m == nil {
		return Pkg{}, false
	}
	return Pkg{Manager: "pip", Name: strings.ToLower(strings.ReplaceAll(m[1], "_", "-")), Version: m[2]}, true
}

// Set is the installed packages, keyed by manager and name.
type Set map[string]Pkg

// Add puts packages in the set; a later version of a name replaces it.
func (s Set) Add(ps ...Pkg) {
	for _, p := range ps {
		s[p.key()] = p
	}
}

// Upgrade is a package whose version changed.
type Upgrade struct {
	Manager string `json:"manager"`
	Name    string `json:"name"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// Diff is how one set of packages differs from another.
type Diff struct {
	Installed []Pkg     `json:"installed"`
	Removed   []Pkg     `json:"removed"`
	Upgraded  []Upgrade `json:"upgraded"`
}

// Empty reports whether nothing changed.
func (d Diff) Empty() bool {
	return len(d.Installed) == 0 && len(d.Removed) == 0 && len(d.Upgraded) == 0
}

// Compare returns what changed from a to b, each list sorted by name.
func Compare(a, b Set) Diff {
	d := Diff{Installed: []Pkg{}, Removed: []Pkg{}, Upgraded: []Upgrade{}}
	for k, p := range b {
		old, ok := a[k]
		switch {
		case !ok:
			d.Installed = append(d.Installed, p)
		case old.Version != p.Version:
			d.Upgraded = append(d.Upgraded, Upgrade{Manager: p.Manager, Name: p.Name, From: old.Version, To: p.Version})
		}
	}
	for k, p := range a {
		if _, ok := b[k]; !ok {
			d.Removed = append(d.Removed, p)
		}
	}
	sort.Slice(d.Installed, func(i, j int) bool { return d.Installed[i].key() < d.Installed[j].key() })
	sort.Slice(d.Removed, func(i, j int) bool { return d.Removed[i].key() < d.Removed[j].key() })
	sort.Slice(d.Upgraded, func(i, j int) bool {
		return d.Upgraded[i].Manager+"/"+d.Upgraded[i].Name < d.Upgraded[j].Manager+"/"+d.Upgraded[j].Name
	})
	return d
}

// Summary says what changed in a few words: "installed curl 8.5.0 and
// git 2.39.5 (apt); upgraded libc6 2.36-9 → 2.36-9+deb12u3".
func (d Diff) Summary() string {
	var parts []string
	if len(d.Installed) > 0 {
		parts = append(parts, "installed "+listPkgs(d.Installed))
	}
	if len(d.Upgraded) > 0 {
		if len(d.Upgraded) <= 2 {
			var us []string
			for _, u := range d.Upgraded {
				us = append(us, fmt.Sprintf("%s %s → %s", u.Name, u.From, u.To))
			}
			parts = append(parts, "upgraded "+strings.Join(us, ", "))
		} else {
			parts = append(parts, fmt.Sprintf("upgraded %d packages", len(d.Upgraded)))
		}
	}
	if len(d.Removed) > 0 {
		parts = append(parts, "removed "+listPkgs(d.Removed))
	}
	return strings.Join(parts, "; ")
}

func listPkgs(ps []Pkg) string {
	managers := map[string]bool{}
	for _, p := range ps {
		managers[p.Manager] = true
	}
	tag := ""
	if len(managers) == 1 {
		tag = " (" + ps[0].Manager + ")"
	}
	switch {
	case len(ps) > 12:
		return fmt.Sprintf("%d packages%s", len(ps), tag)
	case len(ps) > 3:
		return fmt.Sprintf("%s, %s and %d more%s", ps[0].Name, ps[1].Name, len(ps)-2, tag)
	}
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name + " " + p.Version
	}
	if len(names) == 1 {
		return names[0] + tag
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + tag
}
