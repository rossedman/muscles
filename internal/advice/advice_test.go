package advice

import (
	"context"
	"strings"
	"testing"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/index"
	"github.com/rossedman/muscles/internal/source"
	"github.com/rossedman/muscles/internal/testimg"
)

func report(t *testing.T, steps ...testimg.Step) *analysis.Report {
	t.Helper()
	ii, err := index.Indexer{}.Index(context.Background(), &source.Image{Ref: "app:1", Image: testimg.Build(steps...)})
	if err != nil {
		t.Fatal(err)
	}
	return analysis.Analyze(ii, analysis.Options{LargeFile: 50_000, PermissionRewrite: 1_000, PackageCache: true, Overwrite: 10_000})
}

func TestApp(t *testing.T) {
	r := report(t, testimg.AppSteps(100)...)
	r.Layers[5].Line = 12
	adv := For(r)
	if len(adv.Fixes) != 3 {
		t.Fatalf("fixes: %+v", adv.Fixes)
	}
	want := []struct {
		kind  string
		layer int
		saves int64
		in    string
	}{
		{analysis.NotePermissionRewrite, 5, 60208, "Stores 3 files, 60 KB again only to change owner or permissions; the first copy is still in L4."},
		{analysis.NoteOverwrite, 1, 20000, "Replaces 1 file, 20 KB that L0 still stores."},
		{analysis.NotePackageCache, 1, 9000, "Leaves the apt lists in the image (2 files, 9 KB). L3 deletes them later, but that frees nothing."},
	}
	for i, w := range want {
		f := adv.Fixes[i]
		if f.Kind != w.kind || f.Layer != w.layer || f.Saves != w.saves || f.Problem != w.in || f.Fix == "" {
			t.Errorf("fix %d = %+v, want %+v", i, f, w)
		}
	}
	if adv.Fixes[0].Line != 12 || !strings.Contains(adv.Fixes[0].Fix, "COPY --chown") || adv.Fixes[0].Step != "RUN chown -R 1000:1000 /app" {
		t.Errorf("top fix: %+v", adv.Fixes[0])
	}
	// Every wasted byte is claimed exactly once.
	if adv.Saves != r.Waste {
		t.Errorf("saves %d, waste %d", adv.Saves, r.Waste)
	}
	if len(adv.Looks) != 1 || adv.Looks[0].Kind != analysis.NoteLargeFile || adv.Looks[0].Saves != 0 || !strings.Contains(adv.Looks[0].Problem, "/app/models/base.onnx") {
		t.Errorf("looks: %+v", adv.Looks)
	}
}

func TestDeleteOnly(t *testing.T) {
	// A deletion of something that isn't a package cache is its own fix.
	r := report(t,
		testimg.Layer("COPY build", testimg.File{Path: "src/big.o", Size: 5000}),
		testimg.Layer("RUN rm -rf /src", testimg.Whiteout("src")),
	)
	adv := For(r)
	if len(adv.Fixes) != 1 || adv.Fixes[0].Kind != analysis.NoteHiddenDelete || adv.Fixes[0].Saves != 5000 || !strings.Contains(adv.Fixes[0].Problem, "doesn't get smaller") || !strings.Contains(adv.Fixes[0].Fix, "(L0)") {
		t.Fatalf("fixes: %+v", adv.Fixes)
	}
}

func TestClean(t *testing.T) {
	adv := For(report(t, testimg.Layer("FROM scratch", testimg.File{Path: "a", Body: "a"})))
	if len(adv.Fixes) != 0 || len(adv.Looks) != 0 || adv.Saves != 0 || adv.Fixes == nil {
		t.Fatalf("clean: %+v", adv)
	}
	if filesAndBytes(1, 10) != "1 file, 10 B" || layerNames(nil) != "an earlier layer" {
		t.Fatal("helpers")
	}
}
