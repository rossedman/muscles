package analysis

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rossedman/muscles/internal/size"
)

// packageCaches are directories package managers leave behind, what to
// call them, and how to keep them out of a layer.
var packageCaches = []struct{ prefix, label, fix string }{
	{"/var/lib/apt/lists/", "apt lists", "End this RUN with `&& rm -rf /var/lib/apt/lists/*`."},
	{"/var/cache/apt/", "apt cache", "End this RUN with `&& apt-get clean`."},
	{"/var/cache/apk/", "apk cache", "Use `apk add --no-cache`."},
	{"/var/cache/yum/", "yum cache", "End this RUN with `&& yum clean all`."},
	{"/var/cache/dnf/", "dnf cache", "End this RUN with `&& dnf clean all`."},
	{"/root/.cache/pip/", "pip cache", "Use `pip install --no-cache-dir` (or set PIP_NO_CACHE_DIR=1)."},
	{"/root/.npm/", "npm cache", "End this RUN with `&& npm cache clean --force`, or use `RUN --mount=type=cache,target=/root/.npm`."},
	{"/usr/local/share/.cache/yarn/", "yarn cache", "End this RUN with `&& yarn cache clean`."},
	{"/root/.cache/go-build/", "Go build cache", "Use `RUN --mount=type=cache,target=/root/.cache/go-build`, or build in an earlier stage."},
}

// CacheFix returns the fix for the package cache under path, if any.
func CacheFix(path string) (label, fix string, ok bool) {
	for _, pc := range packageCaches {
		if strings.HasPrefix(path+"/", pc.prefix) || strings.HasPrefix(path, pc.prefix) {
			return pc.label, pc.fix, true
		}
	}
	return "", "", false
}

// summarize renders the directories that hold most of a layer's bytes, like "mostly /usr (12 MB), /opt (3 MB)".
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

// layerList returns the layers in set, sorted, and their names.
func layerList(set map[int]bool) ([]int, string) {
	var ls []int
	for l := range set {
		ls = append(ls, l)
	}
	sort.Ints(ls)
	return ls, LayerNames(ls)
}

// LayerNames names sorted layers: "L1", "L1 and L3", "L0, L1 and L3".
func LayerNames(ls []int) string {
	names := make([]string, len(ls))
	for i, l := range ls {
		names[i] = fmt.Sprintf("L%d", l)
	}
	switch len(names) {
	case 0:
		return "an earlier layer"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// rewriteFix says how to avoid storing files again to change ownership,
// naming the command the step ran when it can tell.
func rewriteFix(step string) string {
	switch {
	case strings.Contains(step, "chown"):
		return "Set the owner as the files are added (`COPY --chown=USER:GROUP`, or chown in the step that creates them) and drop this chown."
	case strings.Contains(step, "chmod"):
		return "Set the mode as the files are added (`COPY --chmod=MODE`, or chmod in the step that creates them) and drop this chmod."
	}
	return "Set owners and permissions in the step that creates the files, not in a later layer."
}

// notes finds what is worth a second look in one layer's changes: package caches, deletions that free nothing, permission-only rewrites and large files.
func notes(changes []Change, step string, o Options) []Note {
	var out []Note
	caches := map[string]int64{}
	cacheFiles := map[string]int{}
	var metaBytes, hidden, replaced int64
	var metaFiles, replacedFiles int
	hiddenIn, metaFrom, replacedFrom := map[int]bool{}, map[int]bool{}, map[int]bool{}
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
			metaFiles++
			if c.PrevLayer >= 0 {
				metaFrom[c.PrevLayer] = true
			}
		case Modified:
			if c.Kind == "file" && c.PrevLayer >= 0 && c.PrevSize > 0 {
				replaced += c.PrevSize
				replacedFiles++
				replacedFrom[c.PrevLayer] = true
			}
		}
		if c.Kind != "file" {
			continue
		}
		if o.PackageCache {
			for _, pc := range packageCaches {
				if strings.HasPrefix(c.Path, pc.prefix) {
					caches[pc.label] += c.Size
					cacheFiles[pc.label]++
				}
			}
		}
		if o.LargeFile > 0 && c.Size >= o.LargeFile && c.Type != Meta {
			large = append(large, Note{
				Kind:    NoteLargeFile,
				Message: fmt.Sprintf("large file %s (%s)", c.Path, size.Format(c.Size, false)),
				Bytes:   c.Size, Path: c.Path, Files: 1,
				Fix: "If it isn't needed at run time, keep it out with .dockerignore or a multi-stage build; if it is, consider mounting it instead.",
			})
		}
	}
	for _, pc := range packageCaches {
		if b := caches[pc.label]; b > 0 {
			out = append(out, Note{Kind: NotePackageCache, Message: fmt.Sprintf("%s left behind: %s", pc.label, size.Format(b, false)), Bytes: b, Files: cacheFiles[pc.label], Path: strings.TrimSuffix(pc.prefix, "/"), Fix: pc.fix})
		}
	}
	if o.PermissionRewrite > 0 && metaBytes >= o.PermissionRewrite {
		ls, _ := layerList(metaFrom)
		out = append(out, Note{Kind: NotePermissionRewrite, Message: fmt.Sprintf("%s permission-only rewrite", size.Format(metaBytes, false)), Bytes: metaBytes, Files: metaFiles, StoredIn: ls, Fix: rewriteFix(step)})
	}
	if o.Overwrite > 0 && replaced >= o.Overwrite {
		ls, where := layerList(replacedFrom)
		out = append(out, Note{Kind: NoteOverwrite, Message: fmt.Sprintf("replaces %s that %s still stores", size.Format(replaced, false), where), Bytes: replaced, Files: replacedFiles, StoredIn: ls, Fix: "Install the final version once: do this in the step that first adds the files, or start from a base image that already has it."})
	}
	if hidden > 0 {
		ls, where := layerList(hiddenIn)
		out = append(out, Note{Kind: NoteHiddenDelete, Message: fmt.Sprintf("deletes %s that %s still stores", size.Format(hidden, false), where), Bytes: hidden, StoredIn: ls, Fix: fmt.Sprintf("Delete these files in the step that creates them (%s), or build them in an earlier stage and copy only what you need.", where)})
	}
	sort.SliceStable(large, func(i, j int) bool { return large[i].Bytes > large[j].Bytes })
	return append(out, large...)
}
