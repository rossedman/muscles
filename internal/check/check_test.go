package check

import (
	"context"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/rossedman/muscles/internal/analysis"
	"github.com/rossedman/muscles/internal/index"
	"github.com/rossedman/muscles/internal/size"
	"github.com/rossedman/muscles/internal/source"
	"github.com/rossedman/muscles/internal/testimg"
)

func analyze(t *testing.T, ref string, img v1.Image) *analysis.Report {
	t.Helper()
	ii, err := index.Indexer{}.Index(context.Background(), &source.Image{Ref: ref, Image: img})
	if err != nil {
		t.Fatal(err)
	}
	return analysis.Analyze(ii, analysis.Options{LargeFile: 50_000, PermissionRewrite: 1_000, PackageCache: true})
}

func lines(r Report) string {
	var s []string
	for _, x := range r.Results {
		line := string(x.Status) + " " + x.Rule + ": " + x.Message
		if x.Detail != "" {
			line += " (" + x.Detail + ")"
		}
		s = append(s, line)
	}
	return strings.Join(s, "\n")
}

func TestRun(t *testing.T) {
	base, img := analyze(t, "app:1", testimg.App(100)), analyze(t, "app:2", testimg.AppNext(100))
	b := Budget{
		MaxSize:       100_000,
		MaxCompressed: 10_000_000,
		MaxWaste:      size.Limit{Bytes: 50_000},
		MaxGrowth:     size.Limit{Percent: 1},
		MaxLayers:     5,
		Forbid:        []string{"**/.env", "/app/api/", "**/*.py", "**/.git/"},
		Allow:         []string{"usr/**"},
	}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
	r := Run(img, base, b, false)
	got := lines(r)
	for _, want := range []string{
		"fail size: 239 KB > 100 KB (biggest layer: L4 COPY . . (61 KB))",
		"pass compressed:",
		"fail waste: 90 KB > 50 KB (biggest: /app/models/base.onnx (60 KB, permission-only rewrite in L5))",
		"pass growth: -12 KB ≤ +2.5 KB vs app:1",
		"fail layers: 6 > 5",
		`pass forbid: nothing matches "**/.env"`,
		`fail forbid: /app/api matches "/app/api/" (added in L4 COPY . .)`,
		`fail forbid: /app/api/retry.py matches "**/*.py" (added in L5 RUN chown -R 1000:1000 /app)`,
		"warn hidden delete: deletes 9 KB that L1 still stores",
		"warn large file: large file /app/models/base.onnx (60 KB) (L4 COPY . .)",
		`pass forbid: nothing matches "**/.git/"`,
		"warn package cache: apt lists left behind: 9 KB (L1 RUN apt-get update && apt-get install -y curl)",
		"warn permission rewrite:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if r.OK() || r.Failed != 6 || r.Warnings != 4 || r.Baseline != "app:1" {
		t.Errorf("report: %+v", r)
	}
	grew := Run(base, img, Budget{MaxGrowth: size.Limit{Percent: 1}}, false)
	if got := lines(grew); !strings.Contains(got, "fail growth: +12 KB > +2.4 KB vs app:2") {
		t.Errorf("growth:\n%s", got)
	}
}

func TestRunEdges(t *testing.T) {
	img := analyze(t, "app:1", testimg.App(100))
	r := Run(img, nil, Budget{MaxGrowth: size.Limit{Bytes: 1}, MaxLayers: 10, MaxSize: 1 << 30, MaxWaste: size.Limit{Percent: 90}}, true)
	got := lines(r)
	for _, want := range []string{"skip growth: no baseline image", "pass layers: 6 ≤ 10", "pass size: 245 KiB ≤ 1 GiB", "pass waste:"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	// Growth within budget, and more than five forbidden hits.
	many := Run(img, img, Budget{MaxGrowth: size.Limit{Bytes: 1}, Forbid: []string{"**"}}, false)
	got = lines(many)
	if !strings.Contains(got, "pass growth: 0 B ≤ +1 B vs app:1") || !strings.Contains(got, "more paths match") {
		t.Errorf("many:\n%s", got)
	}
	if err := (Budget{Forbid: []string{"[bad"}}).Validate(); err == nil {
		t.Error("bad pattern accepted")
	}
	for s, want := range map[Status]string{Pass: "✓", Fail: "✗", Warn: "⚠", Skip: "·"} {
		if s.Symbol() != want {
			t.Errorf("%s symbol", s)
		}
	}
	empty := &analysis.Report{Ref: "x"}
	if biggestLayer(empty, false) != "" || biggestWaste(empty, false) != "" {
		t.Error("empty biggest")
	}
	if biggestWaste(&analysis.Report{WasteItems: []analysis.WasteItem{{Path: "/f", Size: 1, Reason: analysis.Deleted, HiddenBy: 2}}}, false) != "biggest: /f (1 B, deleted in L2)" {
		t.Error("file waste")
	}
}

func TestFixes(t *testing.T) {
	base, img := analyze(t, "app:1", testimg.App(100)), analyze(t, "app:2", testimg.AppNext(100))
	img.Layers[4].Line = 9
	r := Run(base, img, Budget{MaxSize: 1, MaxGrowth: size.Limit{Bytes: 1}, MaxLayers: 1, Forbid: []string{"**/*.onnx"}}, false)
	fixes := map[string]Result{}
	for _, x := range r.Results {
		if x.Status == Fail || x.Status == Warn {
			fixes[x.Rule] = x
		}
	}
	if !strings.Contains(fixes["size"].Fix, "muscles images inspect app:1") {
		t.Errorf("size fix: %+v", fixes["size"])
	}
	if !strings.Contains(fixes["growth"].Fix, "muscles images diff app:2 app:1") {
		t.Errorf("growth fix: %+v", fixes["growth"])
	}
	if fixes["layers"].Fix == "" {
		t.Error("layers fix")
	}
	if f := fixes["forbid"]; !strings.Contains(f.Fix, ".dockerignore") || f.Layer == nil || *f.Layer != 5 {
		t.Errorf("forbid fix: %+v", f)
	}
	if f := fixes["large file"]; f.Fix == "" || f.Layer == nil || *f.Layer != 4 {
		t.Errorf("note fix: %+v", f)
	}
	// Dockerfile lines come from the checked image.
	r = Run(img, nil, Budget{}, false)
	for _, x := range r.Results {
		if x.Rule == "large file" && x.Line != 9 {
			t.Errorf("line: %+v", x)
		}
	}
}
