package compare

import (
	"context"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/index"
	"github.com/rossedman/muscles/internal/source"
	"github.com/rossedman/muscles/internal/testimg"
)

func analyze(t *testing.T, ref string, img v1.Image) *analysis.Report {
	t.Helper()
	ii, err := index.Indexer{}.Index(context.Background(), &source.Image{Ref: ref, Image: img})
	if err != nil {
		t.Fatal(err)
	}
	return analysis.Analyze(ii, analysis.DefaultOptions())
}

func TestImages(t *testing.T) {
	a, b := analyze(t, "app:1", testimg.App(100)), analyze(t, "app:2", testimg.AppNext(100))
	r := Images(a, b)
	if r.Shared != 3 || len(r.Layers) != 6 || !r.Layers[2].Shared || r.Layers[3].Shared {
		t.Fatalf("layers: shared %d %+v", r.Shared, r.Layers)
	}
	if r.Layers[3].Step() != "RUN pip install -r requirements.txt && rm -rf /var/lib/apt/lists/*" {
		t.Fatalf("step %q", r.Layers[3].Step())
	}
	var got []string
	for _, c := range r.Changes {
		got = append(got, string(c.Type)+" "+c.Path)
	}
	want := "deleted /app/.env|added /app/api/retry.py|modified /app/main.py|deleted /usr/local/lib/python3.12/site-packages/numpy/core.so|modified /usr/local/lib/python3.12/site-packages/pandas/core.py"
	if strings.Join(got, "|") != want {
		t.Fatalf("changes:\n%s\nwant\n%s", strings.Join(got, "|"), want)
	}
	if r.SizeDelta() != b.Size-a.Size || r.CompressedDelta() != b.Compressed-a.Compressed {
		t.Fatal("deltas")
	}
}

func TestUnevenAndFilesystems(t *testing.T) {
	short := analyze(t, "s", testimg.Build(testimg.Layer("FROM", testimg.File{Path: "a", Body: "a"})))
	long := analyze(t, "l", testimg.Build(testimg.Layer("FROM", testimg.File{Path: "a", Body: "a"}), testimg.Layer("RUN", testimg.File{Path: "b", Body: "b"})))
	r := Images(long, short)
	if r.Shared != 1 || r.Layers[1].Target != nil || r.Layers[1].Step() != "RUN" {
		t.Fatalf("uneven: %+v", r.Layers)
	}

	fa := map[string]analysis.FinalEntry{
		"/d":       {Entry: index.Entry{Path: "/d", Type: index.Dir}},
		"/d/f":     {Entry: index.Entry{Path: "/d/f", Size: 1, Hash: 1}},
		"/empty":   {Entry: index.Entry{Path: "/empty", Type: index.Dir}},
		"/mode":    {Entry: index.Entry{Path: "/mode", Mode: 0o644}},
		"/dirmode": {Entry: index.Entry{Path: "/dirmode", Type: index.Dir, Mode: 0o755}},
	}
	fb := map[string]analysis.FinalEntry{
		"/n":       {Entry: index.Entry{Path: "/n", Type: index.Dir}},
		"/n/f":     {Entry: index.Entry{Path: "/n/f", Size: 1, Hash: 1}},
		"/new":     {Entry: index.Entry{Path: "/new", Type: index.Dir}},
		"/mode":    {Entry: index.Entry{Path: "/mode", Mode: 0o755}},
		"/dirmode": {Entry: index.Entry{Path: "/dirmode", Type: index.Dir, Mode: 0o700}},
	}
	var got []string
	for _, c := range Filesystems(fa, fb) {
		got = append(got, string(c.Type)+" "+c.Path)
	}
	if strings.Join(got, "|") != "deleted /d/f|deleted /empty|meta /mode|added /n/f|added /new" {
		t.Fatalf("filesystems: %v", got)
	}
}
