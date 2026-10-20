package analysis

import (
	"context"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/rossedman/muscles/internal/index"
	"github.com/rossedman/muscles/internal/source"
	"github.com/rossedman/muscles/internal/testimg"
)

func indexed(t *testing.T, img v1.Image) *index.Image {
	t.Helper()
	ii, err := index.Indexer{}.Index(context.Background(), &source.Image{Ref: "app:dev", Kind: source.Tar, Platform: "linux/amd64", Image: img})
	if err != nil {
		t.Fatal(err)
	}
	return ii
}

func testOptions() Options {
	return Options{LargeFile: 50_000, PermissionRewrite: 1_000, PackageCache: true}
}

func TestAnalyzeApp(t *testing.T) {
	r := Analyze(indexed(t, testimg.App(100)), testOptions())
	if len(r.Layers) != 6 || r.Ref != "app:dev" || r.Source != "tar" || r.Platform != "linux/amd64" || !strings.HasPrefix(r.Digest, "sha256:") {
		t.Fatalf("report: %+v", r)
	}
	wantCounts := []Counts{{Added: 4}, {Added: 3, Modified: 1}, {}, {Added: 4, Deleted: 2}, {Added: 3}, {Meta: 3}}
	wantSteps := []string{"ADD file:4f2a in /", "RUN apt-get update && apt-get install -y curl", "WORKDIR /app", "RUN pip install -r requirements.txt && rm -rf /var/lib/apt/lists/*", "COPY . .", "RUN chown -R 1000:1000 /app"}
	for i, l := range r.Layers {
		if l.Counts != wantCounts[i] {
			t.Errorf("layer %d counts = %+v, want %+v", i, l.Counts, wantCounts[i])
		}
		if l.Step != wantSteps[i] {
			t.Errorf("layer %d step = %q, want %q", i, l.Step, wantSteps[i])
		}
	}
	if got := r.Layers[1].EmptyBefore; len(got) != 1 || got[0] != "ENV PYTHONUNBUFFERED=1" {
		t.Errorf("empty before L1: %q", got)
	}
	if len(r.EmptyAfter) != 1 || r.EmptyAfter[0] != `CMD ["python" "main.py"]` {
		t.Errorf("empty after: %q", r.EmptyAfter)
	}

	// Waste: libc replaced (20000), apt lists deleted (9000), chown (60208).
	if r.Waste != 89208 {
		t.Errorf("waste = %d", r.Waste)
	}
	if r.Layers[0].Wasted != 20000 || r.Layers[1].Wasted != 9000 || r.Layers[4].Wasted != 60208 {
		t.Errorf("wasted per layer: %d %d %d", r.Layers[0].Wasted, r.Layers[1].Wasted, r.Layers[4].Wasted)
	}
	if w := r.WasteItems[0]; w.Path != "/app/models/base.onnx" || w.Reason != Meta || w.StoredIn != 4 || w.HiddenBy != 5 {
		t.Errorf("top waste: %+v", w)
	}
	if r.Size != 32200+33000+0+65100+60208+60208 {
		t.Errorf("size = %d", r.Size)
	}

	// Summaries and notes.
	if got := r.Layers[3].Summary; got != "mostly /usr/local/lib/python3.12/site-packages (65 KB)" {
		t.Errorf("L3 summary = %q", got)
	}
	notes := func(i int) string {
		var s []string
		for _, n := range r.Layers[i].Notes {
			s = append(s, n.Kind+": "+n.Message)
		}
		return strings.Join(s, "; ")
	}
	if got := notes(1); got != "package_cache: apt lists left behind: 9 KB" {
		t.Errorf("L1 notes = %q", got)
	}
	if got := notes(3); got != "hidden_delete: deletes 9 KB that L1 still stores" {
		t.Errorf("L3 notes = %q", got)
	}
	if got := notes(4); got != "large_file: large file /app/models/base.onnx (60 KB)" {
		t.Errorf("L4 notes = %q", got)
	}
	if got := notes(5); got != "permission_rewrite: 60 KB permission-only rewrite" {
		t.Errorf("L5 notes = %q", got)
	}
	if r.Layers[2].Summary != "" || len(r.Layers[2].Notes) != 0 {
		t.Errorf("L2: %+v", r.Layers[2])
	}

	// Final filesystem.
	for _, p := range []string{"/app/.env", "/usr/bin/curl", "/usr/bin/sh", "/var/lib/apt/lists", "/usr/local/lib/python3.12/site-packages"} {
		if _, ok := r.Final[p]; !ok {
			t.Errorf("final lacks %s", p)
		}
	}
	if _, ok := r.Final["/var/lib/apt/lists/deb_main_Packages"]; ok {
		t.Error("deleted file in final")
	}
	if f := r.Final["/app/.env"]; f.Layer != 5 || f.UID != 1000 {
		t.Errorf(".env: %+v", f)
	}
	if f := r.Final["/usr/local/lib/python3.12"]; f.Type != index.Dir || f.Layer != 3 {
		t.Errorf("implied dir: %+v", f)
	}
}

func TestRollup(t *testing.T) {
	r := Analyze(indexed(t, testimg.App(100)), testOptions())
	rows := RollupChanges(r.Layers[3].Changes, DefaultLimit)
	var got []string
	for _, row := range rows {
		got = append(got, row.Type.Symbol()+" "+row.Path)
	}
	want := "+ /usr/local/lib/python3.12/site-packages/pandas/core.py|+ /usr/local/lib/python3.12/site-packages/numpy/core.so|+ /usr/local/lib/python3.12/site-packages/pandas/io.py|- /var/lib/apt/lists/deb_main_Packages|- /var/lib/apt/lists/deb_updates_Packages|+ /usr/local/lib/python3.12/site-packages/six.py"
	if strings.Join(got, "|") != want {
		t.Errorf("rows:\n%s\nwant\n%s", strings.Join(got, "|"), want)
	}

	rows = RollupChanges(r.Layers[3].Changes, 3)
	got = nil
	for _, row := range rows {
		got = append(got, row.Path)
		if row.Dir && (row.Kind != "dir" || row.Count < 2) {
			t.Errorf("dir row: %+v", row)
		}
	}
	if strings.Join(got, "|") != "/usr/local/lib/python3.12/site-packages|/var/lib/apt/lists/deb_main_Packages|/var/lib/apt/lists/deb_updates_Packages" {
		t.Errorf("limit 3: %v", got)
	}
	if rows[0].Type != Added || !rows[0].Dir || rows[0].Count != 4 || rows[0].Size != 65100 || rows[0].PrevLayer != -1 {
		t.Errorf("dir row: %+v", rows[0])
	}

	if all := RollupChanges(r.Layers[3].Changes, 0); len(all) != 6 {
		t.Errorf("no limit: %d rows", len(all))
	}
	if RollupChanges(nil, 10) != nil {
		t.Error("empty rollup")
	}

	// Mixed reasons force the root open even past the limit: chown (meta),
	// libc (modified) and the apt lists (deleted).
	w := RollupWaste(r.WasteItems, 2)
	if len(w) != 3 || !w[0].Dir || w[0].Path != "/app" || w[0].Count != 3 || w[0].Reason != Meta || w[1].Path != "/usr/lib/libc.so.6" || w[2].Path != "/var/lib/apt/lists" {
		t.Errorf("waste rows: %+v", w)
	}
	w = RollupWaste(r.WasteItems, DefaultLimit)
	if len(w) != 6 || w[0].Path != "/app/models/base.onnx" || w[0].Reason != Meta {
		t.Errorf("waste rows: %+v", w)
	}
}

func TestRollupEdges(t *testing.T) {
	changes := []Change{
		{Path: "/app", Type: Deleted, Kind: "dir", Size: 10, PrevLayer: 0},
		{Path: "/app/x", Type: Added, Kind: "file", Size: 5, PrevLayer: -1},
		{Path: "/app/x", Type: Modified, Kind: "file", Size: 6, PrevSize: 5, PrevLayer: 1},
		{Path: "/", Type: Deleted, Kind: "dir", Size: 1, PrevLayer: -1},
	}
	rows := RollupChanges(changes, DefaultLimit)
	if len(rows) != 4 || rows[0].Path != "/app" || rows[0].Type != Deleted {
		t.Fatalf("rows: %+v", rows)
	}
	// One uniform directory with one file is shown as the file.
	rows = RollupChanges([]Change{{Path: "/a/b/c", Type: Added, Size: 1}}, 1)
	if len(rows) != 1 || rows[0].Dir || rows[0].Path != "/a/b/c" {
		t.Fatalf("single: %+v", rows)
	}
}

func TestCleanStep(t *testing.T) {
	cases := map[string]string{
		"/bin/sh -c #(nop)  ENV A=1":                   "ENV A=1",
		"/bin/sh -c apt-get   update":                  "RUN apt-get update",
		"RUN |2 A=1 B= /bin/sh -c make # buildkit":     "RUN make",
		"RUN /bin/sh -c make # buildkit":               "RUN make",
		"COPY --from=build /out /app # buildkit":       "COPY --from=build /out /app",
		"RUN |0 make":                                  "RUN make",
		"":                                             "",
		"WORKDIR /app":                                 "WORKDIR /app",
		"RUN |1 X=1 /usr/bin/env bash -c true":         "RUN /usr/bin/env bash -c true",
		"/bin/sh -c #(nop) ADD file:4f2a in / ":        "ADD file:4f2a in /",
		"RUN   echo\thi":                               "RUN echo hi",
		"RUN |1 ARG=value echo done # buildkit":        "RUN echo done",
		"/bin/sh -c #(nop)  CMD [\"python\" \"m.py\"]": "CMD [\"python\" \"m.py\"]",
	}
	for in, want := range cases {
		if got := CleanStep(in); got != want {
			t.Errorf("CleanStep(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPairSteps(t *testing.T) {
	hist := []index.Step{{CreatedBy: "ENV X=1", Empty: true}, {CreatedBy: "RUN a"}, {CreatedBy: "RUN b"}}
	// More layers than history: base layers get no step.
	steps, _, before, after := pairSteps(hist, 3)
	if steps[0] != "" || steps[1] != "RUN a" || steps[2] != "RUN b" || len(before[1]) != 1 || after != nil {
		t.Fatalf("short history: %q %q %q", steps, before, after)
	}
	// More history than layers: the oldest entries are dropped.
	steps, _, _, _ = pairSteps(hist, 1)
	if steps[0] != "RUN b" {
		t.Fatalf("long history: %q", steps)
	}
	steps, _, _, _ = pairSteps(nil, 2)
	if len(steps) != 2 || steps[0] != "" {
		t.Fatalf("no history: %q", steps)
	}
}

func TestEdgeCases(t *testing.T) {
	img := testimg.Build(
		testimg.Layer("base",
			testimg.Dir("data"),
			testimg.File{Path: "data/a", Body: "aaaa"},
			testimg.File{Path: "data/sub/b", Body: "bb"},
			testimg.File{Path: "conf", Body: "c"},
			testimg.File{Path: "link", Type: '2', Link: "conf"},
			testimg.Dir("keep"),
		),
		testimg.Layer("replace",
			// A whiteout for something that never existed changes nothing.
			testimg.Whiteout("nothing/here"),
			testimg.Whiteout("missing"),
			// Opaque on a missing or empty directory changes nothing.
			testimg.Opaque("ghost"),
			testimg.Opaque("keep"),
			// Opaque keeps this layer's own files.
			testimg.Dir("data"),
			testimg.File{Path: "data/new", Body: "n"},
			testimg.Opaque("data"),
			// A file replaced by a directory, and a directory by a file.
			testimg.Dir("conf"),
			testimg.File{Path: "keep", Body: "now a file"},
			// A symlink retargeted, and a parent path through a file.
			testimg.File{Path: "link", Type: '2', Link: "other"},
			testimg.File{Path: "conf/inner", Body: "i"},
			testimg.File{Path: "data/new/x", Body: "x"},
		),
		testimg.Layer("redeclare", testimg.Dir("conf"), testimg.Opaque("conf/inner")),
	)
	r := Analyze(indexed(t, img), Options{})
	l := r.Layers[1]
	var got []string
	for _, c := range l.Changes {
		got = append(got, string(c.Type)+" "+c.Path)
	}
	want := "added /data/new|deleted /data|modified /conf|modified /keep|modified /link|added /conf/inner|added /data/new/x"
	if strings.Join(got, "|") != want {
		t.Errorf("changes:\n%s\nwant\n%s", strings.Join(got, "|"), want)
	}
	if l.Changes[1].Size != 6 {
		t.Errorf("opaque freed %d", l.Changes[1].Size)
	}
	if _, ok := r.Final["/data/new/x"]; !ok {
		t.Error("lost /data/new/x")
	}
	if _, ok := r.Final["/data/a"]; ok {
		t.Error("opaque kept /data/a")
	}
	if len(r.Layers[2].Changes) != 0 {
		t.Errorf("redeclare changes: %+v", r.Layers[2].Changes)
	}
	if mostly(nil) != nil {
		t.Error("mostly(nil)")
	}
}

func TestMostlyAndNotes(t *testing.T) {
	// No directory dominates: the top three directories.
	m := mostly([]Change{
		{Path: "/a/1", Size: 40, Type: Added},
		{Path: "/b/1", Size: 30, Type: Added},
		{Path: "/c/1", Size: 20, Type: Added},
		{Path: "/d/1", Size: 10, Type: Added},
		{Path: "/file", Size: 5, Type: Added},
	})
	if len(m) != 3 || m[0].Path != "/a" || m[2].Path != "/c" {
		t.Fatalf("mostly: %+v", m)
	}
	if s := summarize(m); s != "mostly /a (40 B), /b (30 B), /c (20 B)" {
		t.Fatalf("summary: %q", s)
	}
	// A deletion with no previous layer still notes the bytes.
	n := notes([]Change{{Path: "/x", Type: Deleted, Size: 5, PrevLayer: -1}}, "", Options{})
	if len(n) != 1 || !strings.Contains(n[0].Message, "an earlier layer") {
		t.Fatalf("notes: %+v", n)
	}
	if ChangeType("deleted").Symbol() != "-" || Meta.Symbol() != "◌" || Modified.Symbol() != "~" {
		t.Fatal("symbols")
	}
	if DefaultOptions().LargeFile != 50_000_000 {
		t.Fatal("defaults")
	}
}

func TestFixesInNotes(t *testing.T) {
	o := testOptions()
	o.Overwrite = 10_000
	r := Analyze(indexed(t, testimg.App(100)), o)
	byKind := map[string]Note{}
	for _, l := range r.Layers {
		for _, n := range l.Notes {
			byKind[n.Kind] = n
		}
	}
	if n := byKind[NoteOverwrite]; n.Message != "replaces 20 KB that L0 still stores" || n.Files != 1 || len(n.StoredIn) != 1 || n.StoredIn[0] != 0 || n.Fix == "" {
		t.Errorf("overwrite: %+v", n)
	}
	if n := byKind[NotePackageCache]; n.Files != 2 || !strings.Contains(n.Fix, "rm -rf /var/lib/apt/lists/*") {
		t.Errorf("package cache: %+v", n)
	}
	if n := byKind[NotePermissionRewrite]; n.Files != 3 || n.StoredIn[0] != 4 || !strings.Contains(n.Fix, "COPY --chown") {
		t.Errorf("rewrite: %+v", n)
	}
	if n := byKind[NoteHiddenDelete]; !strings.Contains(n.Fix, "(L1)") {
		t.Errorf("hidden delete: %+v", n)
	}
	if n := byKind[NoteLargeFile]; n.Fix == "" || n.Files != 1 {
		t.Errorf("large file: %+v", n)
	}

	if !strings.Contains(rewriteFix("RUN chmod -R 755 /app"), "--chmod") || !strings.Contains(rewriteFix("RUN setcap x"), "in the step that creates") {
		t.Error("rewriteFix")
	}
	if label, fix, ok := CacheFix("/var/cache/apk"); !ok || label != "apk cache" || !strings.Contains(fix, "--no-cache") {
		t.Errorf("CacheFix: %q %q %v", label, fix, ok)
	}
	if _, _, ok := CacheFix("/srv"); ok {
		t.Error("CacheFix matched /srv")
	}
	for in, want := range map[string][]int{"an earlier layer": nil, "L2": {2}, "L1 and L3": {1, 3}, "L0, L1 and L3": {0, 1, 3}} {
		if got := LayerNames(want); got != in {
			t.Errorf("LayerNames(%v) = %q", want, got)
		}
	}
}

func TestPackages(t *testing.T) {
	status := func(pkgs ...string) testimg.File {
		var b strings.Builder
		for i := 0; i < len(pkgs); i += 2 {
			b.WriteString("Package: " + pkgs[i] + "\nStatus: install ok installed\nVersion: " + pkgs[i+1] + "\n\n")
		}
		return testimg.File{Path: "var/lib/dpkg/status", Body: b.String()}
	}
	img := testimg.Build(
		testimg.Layer("FROM debian", status("bash", "5.2", "libc6", "2.36-9")),
		testimg.Layer("RUN apt-get install curl", status("bash", "5.2", "curl", "7.88", "libc6", "2.36-9+u3")),
		testimg.Layer("RUN pip install pandas",
			testimg.File{Path: "usr/lib/python3/site-packages/pandas-2.2.3.dist-info/METADATA", Body: "x"},
			testimg.File{Path: "usr/lib/python3/site-packages/numpy-2.1.0.dist-info/METADATA", Body: "x"},
		),
		testimg.Layer("RUN pip uninstall numpy", testimg.Whiteout("usr/lib/python3/site-packages/numpy-2.1.0.dist-info")),
		testimg.Layer("RUN rm dpkg", testimg.Dir("var/lib/dpkg"), testimg.Opaque("var/lib/dpkg")),
		testimg.Layer("COPY app", testimg.File{Path: "app/main", Body: "m"}),
	)
	r := Analyze(indexed(t, img), Options{})
	want := []string{
		"installed bash 5.2 and libc6 2.36-9 (dpkg)",
		"installed curl 7.88 (dpkg); upgraded libc6 2.36-9 → 2.36-9+u3",
		"installed numpy 2.1.0 and pandas 2.2.3 (pip)",
		"removed numpy 2.1.0 (pip)",
		"removed bash 5.2, curl 7.88 and libc6 2.36-9+u3 (dpkg)",
		"mostly /app (1 B)",
	}
	for i, w := range want {
		if got := r.Layers[i].Summary; got != w {
			t.Errorf("L%d summary = %q, want %q", i, got, w)
		}
	}
	if r.Layers[5].Packages != nil || r.Layers[1].Packages == nil || len(r.Layers[1].Packages.Upgraded) != 1 {
		t.Errorf("packages: %+v %+v", r.Layers[5].Packages, r.Layers[1].Packages)
	}
	if len(r.Packages) != 1 || r.Packages["pip/pandas"].Version != "2.2.3" {
		t.Errorf("final packages: %+v", r.Packages)
	}
	// A database replaced by a file that declares nothing drops its packages.
	img = testimg.Build(
		testimg.Layer("FROM", status("bash", "5.2")),
		testimg.Layer("RUN truncate", testimg.File{Path: "var/lib/dpkg/status", Size: pkgdbMax()}),
	)
	if r := Analyze(indexed(t, img), Options{}); len(r.Packages) != 0 || r.Layers[1].Summary != "removed bash 5.2 (dpkg)" {
		t.Errorf("replaced: %+v %q", r.Packages, r.Layers[1].Summary)
	}
}
