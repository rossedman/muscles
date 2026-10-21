package compare

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/index"
	"github.com/rossedman/muscles/internal/source"
	"github.com/rossedman/muscles/internal/testimg"
)

// releaseSteps is the App (or AppNext) image with an install layer of n
// files in n/40 package directories. The next release differs in
// every fourth file's contents, every fourth file's owner and a few
// packages added or dropped, the way two builds of one app do.
func releaseSteps(next bool, n int) []testimg.Step {
	steps := testimg.AppSteps(1000)
	if next {
		steps = testimg.AppNextSteps(1000)
	}
	var files []testimg.File
	for i := range n {
		pkg := i / 40
		if next && pkg%10 == 9 {
			pkg += 1000 // a package only the new release has
		}
		f := testimg.File{Path: fmt.Sprintf("usr/local/lib/node_modules/pkg%04d/lib/file%05d.js", pkg, i), Body: strings.Repeat(string(rune('a'+i%26)), 200+i%3000)}
		if next && i%4 == 1 {
			f.Body += "v2"
		}
		if next && i%4 == 3 {
			f.UID = 1000
		}
		files = append(files, f)
	}
	return append(steps[:len(steps)-1], testimg.Layer("RUN /bin/sh -c npm ci", files...), steps[len(steps)-1])
}

// benchReport indexes and analyzes a release, outside the timed loop.
func benchReport(b *testing.B, ref string, next bool, n int) *analysis.Report {
	b.Helper()
	ii, err := index.Indexer{}.Index(b.Context(), &source.Image{Ref: ref, Image: testimg.Build(releaseSteps(next, n)...)})
	if err != nil {
		b.Fatal(err)
	}
	return analysis.Analyze(ii, analysis.DefaultOptions())
}

// BenchmarkImages measures comparing two releases of an image with a few
// thousand files: shared layers, final filesystem changes and packages.
func BenchmarkImages(b *testing.B) {
	base, target := benchReport(b, "app:1", false, 4000), benchReport(b, "app:2", true, 4000)
	if r := Images(base, target); r.Shared != 3 || len(r.Changes) < 2000 {
		b.Fatalf("shared %d, %d changes", r.Shared, len(r.Changes))
	}
	b.ReportAllocs()
	for b.Loop() {
		Images(base, target)
	}
}

// BenchmarkFilesystems measures diffing two final filesystems, the bulk of
// Images' work.
func BenchmarkFilesystems(b *testing.B) {
	base, target := benchReport(b, "app:1", false, 4000), benchReport(b, "app:2", true, 4000)
	if len(base.Final) < 4000 || len(target.Final) < 4000 {
		b.Fatalf("final filesystems of %d and %d paths", len(base.Final), len(target.Final))
	}
	b.ReportAllocs()
	for b.Loop() {
		if len(Filesystems(base.Final, target.Final)) == 0 {
			b.Fatal("no changes")
		}
	}
}
