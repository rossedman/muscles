package analysis

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rossedman/muscles/internal/size"
)

// packageCaches are directories package managers leave behind, and what to
// call them.
var packageCaches = []struct{ prefix, label string }{
	{"/var/lib/apt/lists/", "apt lists"},
	{"/var/cache/apt/", "apt cache"},
	{"/var/cache/apk/", "apk cache"},
	{"/var/cache/yum/", "yum cache"},
	{"/var/cache/dnf/", "dnf cache"},
	{"/root/.cache/pip/", "pip cache"},
	{"/root/.npm/", "npm cache"},
	{"/usr/local/share/.cache/yarn/", "yarn cache"},
	{"/root/.cache/go-build/", "Go build cache"},
}

func summarize(m []DirBytes) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, len(m))
	for i, d := range m {
		parts[i] = fmt.Sprintf("%s (%s)", d.Path, size.Format(d.Bytes, false))
	}
	return "mostly " + strings.Join(parts, ", ")
}

func notes(changes []Change, o Options) []Note {
	var out []Note
	caches := map[string]int64{}
	var metaBytes, hidden int64
	hiddenIn := map[int]bool{}
	var large []Note
	for _, c := range changes {
		switch c.Type {
		case Deleted:
			hidden += c.Size
			if c.Size > 0 && c.PrevLayer >= 0 {
				hiddenIn[c.PrevLayer] = true
			}
			continue
		case Meta:
			metaBytes += c.Size
		}
		if c.Kind != "file" {
			continue
		}
		if o.PackageCache {
			for _, pc := range packageCaches {
				if strings.HasPrefix(c.Path, pc.prefix) {
					caches[pc.label] += c.Size
				}
			}
		}
		if o.LargeFile > 0 && c.Size >= o.LargeFile && c.Type != Meta {
			large = append(large, Note{Kind: NoteLargeFile, Message: fmt.Sprintf("large file %s (%s)", c.Path, size.Format(c.Size, false)), Bytes: c.Size, Path: c.Path})
		}
	}
	for _, pc := range packageCaches {
		if b := caches[pc.label]; b > 0 {
			out = append(out, Note{Kind: NotePackageCache, Message: fmt.Sprintf("%s left behind: %s", pc.label, size.Format(b, false)), Bytes: b, Path: strings.TrimSuffix(pc.prefix, "/")})
		}
	}
	if o.PermissionRewrite > 0 && metaBytes >= o.PermissionRewrite {
		out = append(out, Note{Kind: NotePermissionRewrite, Message: fmt.Sprintf("%s permission-only rewrite", size.Format(metaBytes, false)), Bytes: metaBytes})
	}
	if hidden > 0 {
		var ls []int
		for l := range hiddenIn {
			ls = append(ls, l)
		}
		sort.Ints(ls)
		where := "an earlier layer"
		if len(ls) > 0 {
			names := make([]string, len(ls))
			for i, l := range ls {
				names[i] = fmt.Sprintf("L%d", l)
			}
			where = strings.Join(names, ", ")
		}
		out = append(out, Note{Kind: NoteHiddenDelete, Message: fmt.Sprintf("deletes %s that %s still stores", size.Format(hidden, false), where), Bytes: hidden})
	}
	sort.SliceStable(large, func(i, j int) bool { return large[i].Bytes > large[j].Bytes })
	return append(out, large...)
}
