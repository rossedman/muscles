package analysis

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rossedman/muscles/internal/index"
	"github.com/rossedman/muscles/internal/source"
	"github.com/rossedman/muscles/internal/testimg"
)

// benchSteps is the App image with a realistic dependency install in the
// middle: n files over n/40 package directories, half of them rewritten by
// a later layer, a quarter deleted and an opaque cache directory.
func benchSteps(n int) []testimg.Step {
	steps := testimg.AppSteps(1000)
	var install, rewrite, cleanup []testimg.File
	for i := range n {
		p := fmt.Sprintf("usr/local/lib/node_modules/pkg%03d/lib/file%05d.js", i/40, i)
		body := strings.Repeat(string(rune('a'+i%26)), 200+i%3000)
		install = append(install, testimg.File{Path: p, Body: body})
		switch i % 4 {
		case 0:
			rewrite = append(rewrite, testimg.File{Path: p, Body: body, UID: 1000})
		case 1:
			rewrite = append(rewrite, testimg.File{Path: p, Body: body + "v2"})
		case 2:
			cleanup = append(cleanup, testimg.Whiteout(p))
		}
	}
	install = append(install, testimg.File{Path: "root/.npm/_cacache/index", Body: strings.Repeat("c", 5000)})
	cleanup = append(cleanup, testimg.Opaque("root/.npm"))
	return append(steps[:len(steps)-1],
		testimg.Layer("RUN /bin/sh -c npm ci", install...),
		testimg.Layer("RUN /bin/sh -c npm run build", rewrite...),
		testimg.Layer("RUN /bin/sh -c npm prune --production && rm -rf /root/.npm", cleanup...),
		steps[len(steps)-1],
	)
}

// benchIndex indexes the benchSteps image once, outside the timed loop.
func benchIndex(b *testing.B, n int) *index.Image {
	b.Helper()
	ii, err := index.Indexer{}.Index(b.Context(), &source.Image{Ref: "app:bench", Kind: source.Tar, Platform: "linux/amd64", Image: testimg.Build(benchSteps(n)...)})
	if err != nil {
		b.Fatal(err)
	}
	return ii
}

// BenchmarkAnalyze measures replaying the layers of an indexed image with
// a few thousand files: changes, waste, summaries, notes and packages.
func BenchmarkAnalyze(b *testing.B) {
	for _, n := range []int{100, 4000} {
		b.Run(fmt.Sprintf("files=%d", n), func(b *testing.B) {
			ii := benchIndex(b, n)
			if r := Analyze(ii, DefaultOptions()); len(r.Layers) != 9 || r.Waste == 0 {
				b.Fatalf("report: %d layers, %d waste", len(r.Layers), r.Waste)
			}
			b.ReportAllocs()
			for b.Loop() {
				Analyze(ii, DefaultOptions())
			}
		})
	}
}

// BenchmarkRollupChanges measures rolling the biggest layer's changes into
// rows, as layers list and changes list do.
func BenchmarkRollupChanges(b *testing.B) {
	r := Analyze(benchIndex(b, 4000), DefaultOptions())
	changes := r.Layers[6].Changes
	if len(changes) < 4000 {
		b.Fatalf("%d changes", len(changes))
	}
	for _, limit := range []int{DefaultLimit, 0} {
		b.Run(fmt.Sprintf("limit=%d", limit), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if len(RollupChanges(changes, limit)) == 0 {
					b.Fatal("no rows")
				}
			}
		})
	}
}

// BenchmarkRollupWaste measures rolling the image's waste into rows, as
// waste list does.
func BenchmarkRollupWaste(b *testing.B) {
	r := Analyze(benchIndex(b, 4000), DefaultOptions())
	if len(r.WasteItems) < 2000 {
		b.Fatalf("%d waste items", len(r.WasteItems))
	}
	for _, limit := range []int{DefaultLimit, 0} {
		b.Run(fmt.Sprintf("limit=%d", limit), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if len(RollupWaste(r.WasteItems, limit)) == 0 {
					b.Fatal("no rows")
				}
			}
		})
	}
}
