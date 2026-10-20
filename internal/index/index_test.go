package index

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/rossedman/muscles/internal/source"
	"github.com/rossedman/muscles/internal/testimg"
)

func TestReadTar(t *testing.T) {
	b := testimg.TarBytes(
		testimg.Dir("./"),
		testimg.Dir("./usr/"),
		testimg.File{Path: "./usr/bin/curl", Body: "curl", Mode: 0o4755, UID: 1, GID: 2},
		testimg.File{Path: "usr/bin/sh", Type: tar.TypeSymlink, Link: "dash"},
		testimg.File{Path: "usr/bin/ln", Type: tar.TypeLink, Link: "./usr/bin/curl"},
		testimg.File{Path: "dev/null", Type: tar.TypeChar},
		testimg.Whiteout("etc/old.conf"),
		testimg.Opaque("var/cache"),
		testimg.File{Path: "var/.wh..wh.plnk"},
	)
	entries, size, err := ReadTar(context.Background(), bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if size != 4 {
		t.Fatalf("size %d", size)
	}
	want := []Entry{
		{Path: "/usr", Type: Dir, Mode: 0o755},
		{Path: "/usr/bin/curl", Type: File, Size: 4, Mode: 0o4755, UID: 1, GID: 2, Hash: entries[1].Hash},
		{Path: "/usr/bin/sh", Type: Symlink, Mode: 0o644, Link: "dash"},
		{Path: "/usr/bin/ln", Type: Hardlink, Mode: 0o644, Link: "/usr/bin/curl"},
		{Path: "/dev/null", Type: Other, Mode: 0o644},
		{Path: "/etc/old.conf", Type: Whiteout},
		{Path: "/var/cache", Type: Opaque},
	}
	if len(entries) != len(want) {
		t.Fatalf("entries: %+v", entries)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, entries[i], want[i])
		}
	}
	if entries[1].Hash == 0 {
		t.Error("no hash")
	}
	if Opaque.String() != "opaque" || File.String() != "file" {
		t.Error("Type.String")
	}

	if _, _, err := ReadTar(context.Background(), strings.NewReader("garbage that is not a tar header, long enough to be read as one block................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................")); err == nil {
		t.Error("garbage accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := ReadTar(ctx, bytes.NewReader(b)); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled: %v", err)
	}
	// A truncated body fails while hashing.
	if _, _, err := ReadTar(context.Background(), bytes.NewReader(b[:1536+2])); err == nil {
		t.Error("truncated tar accepted")
	}
}

func open(t *testing.T, img v1.Image) *source.Image {
	t.Helper()
	return &source.Image{Ref: "app:dev", Kind: source.Tar, Platform: "linux/amd64", Image: img}
}

func TestIndexAndCache(t *testing.T) {
	img := testimg.Build(
		testimg.Layer("FROM scratch", testimg.File{Path: "a", Body: "aaaa"}),
		testimg.Empty("ENV A=1"),
		testimg.Layer("RUN b", testimg.File{Path: "b", Body: "bb"}),
	)
	cache := &Cache{Dir: t.TempDir(), MaxBytes: 1 << 20}
	var events []Progress
	tick := time.Unix(0, 0)
	ix := Indexer{Cache: cache, Progress: func(p Progress) { events = append(events, p) }, Now: func() time.Time { tick = tick.Add(time.Second); return tick }}

	got, err := ix.Index(context.Background(), open(t, img))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Layers) != 2 || got.Layers[0].Size != 4 || got.Layers[1].Size != 2 || len(got.History) != 3 || !got.History[1].Empty || got.Compressed == 0 || !strings.HasPrefix(got.Digest, "sha256:") {
		t.Fatalf("index: %+v", got)
	}
	if len(events) != 4 || events[0].Done || !events[1].Done || events[1].Elapsed != time.Second || events[1].Cached {
		t.Fatalf("events: %+v", events)
	}

	events = nil
	again, err := ix.Index(context.Background(), open(t, img))
	if err != nil || len(events) != 2 || !events[0].Cached || again.Layers[0].Digest != got.Layers[0].Digest {
		t.Fatalf("cached: %+v %v", events, err)
	}
	// A registry reports digests itself, so cached layers still ask it.
	remoteImg := open(t, img)
	remoteImg.Kind = source.Remote
	if r, err := ix.Index(context.Background(), remoteImg); err != nil || r.Layers[1].Digest != got.Layers[1].Digest {
		t.Fatalf("remote cached: %v", err)
	}
	events = nil
	ix.Fresh = true
	if _, err := ix.Index(context.Background(), open(t, img)); err != nil || len(events) != 4 || events[1].Cached {
		t.Fatalf("fresh: %+v %v", events, err)
	}

	list, err := cache.List()
	if err != nil || len(list) != 2 || list[0].Bytes == 0 {
		t.Fatalf("list: %+v %v", list, err)
	}
	removed, err := cache.Prune(0)
	if err != nil || len(removed) != 2 {
		t.Fatalf("prune: %+v %v", removed, err)
	}
	if list, _ := cache.List(); len(list) != 0 {
		t.Fatalf("after prune: %+v", list)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Indexer{}).Index(ctx, open(t, img)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}

func TestCacheEdges(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{Dir: dir}
	if l, err := c.List(); err != nil || l != nil {
		t.Fatalf("empty list: %v %v", l, err)
	}
	if _, ok := c.Get("sha256:nope"); ok {
		t.Fatal("got missing entry")
	}
	if err := c.Put(&Layer{DiffID: "sha256:aaa", Entries: []Entry{{Path: "/x"}}}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(c.file("sha256:bbb"), []byte("not zstd"), 0o644)
	if _, ok := c.Get("sha256:bbb"); ok {
		t.Fatal("got corrupt entry")
	}
	os.Mkdir(filepath.Join(c.dir(), "sub.idx"), 0o755)
	os.WriteFile(filepath.Join(c.dir(), "README"), nil, 0o644)
	// An entry stored under the wrong name is ignored.
	b, _ := os.ReadFile(c.file("sha256:aaa"))
	os.WriteFile(c.file("sha256:ccc"), b, 0o644)
	if _, ok := c.Get("sha256:ccc"); ok {
		t.Fatal("got mismatched entry")
	}
	if l, ok := c.Get("sha256:aaa"); !ok || l.Entries[0].Path != "/x" {
		t.Fatal("lost entry")
	}
	list, _ := c.List()
	if len(list) != 3 || list[0].DiffID != "sha256:aaa" {
		t.Fatalf("list: %+v", list)
	}

	// A cache directory that is a file can't be written or listed.
	f := filepath.Join(dir, "file")
	os.WriteFile(f, nil, 0o644)
	bad := &Cache{Dir: f}
	if err := bad.Put(&Layer{DiffID: "sha256:x"}); err == nil {
		t.Fatal("put into a file")
	}
	if _, err := bad.List(); err == nil {
		t.Fatal("listed a file")
	}
	if _, err := bad.Prune(0); err == nil {
		t.Fatal("pruned a file")
	}
}

// broken fails at one stage of indexing.
type broken struct {
	v1.Image
	stage string
}

func (b broken) ConfigFile() (*v1.ConfigFile, error) {
	if b.stage == "config" {
		return nil, errors.New("boom")
	}
	return b.Image.ConfigFile()
}

func (b broken) Digest() (v1.Hash, error) {
	if b.stage == "digest" {
		return v1.Hash{}, errors.New("boom")
	}
	return b.Image.Digest()
}

func (b broken) Layers() ([]v1.Layer, error) {
	if b.stage == "layers" {
		return nil, errors.New("boom")
	}
	ls, err := b.Image.Layers()
	if err != nil {
		return nil, err
	}
	out := make([]v1.Layer, len(ls))
	for i, l := range ls {
		out[i] = brokenLayer{l, b.stage}
	}
	return out, nil
}

type brokenLayer struct {
	v1.Layer
	stage string
}

func (b brokenLayer) DiffID() (v1.Hash, error) {
	if b.stage == "diffid" {
		return v1.Hash{}, errors.New("boom")
	}
	return b.Layer.DiffID()
}

func (b brokenLayer) Digest() (v1.Hash, error) {
	if b.stage == "ldigest" {
		return v1.Hash{}, errors.New("boom")
	}
	return b.Layer.Digest()
}

func (b brokenLayer) Size() (int64, error) {
	if b.stage == "size" {
		return 0, errors.New("boom")
	}
	return b.Layer.Size()
}

func (b brokenLayer) Uncompressed() (io.ReadCloser, error) {
	switch b.stage {
	case "open":
		return nil, errors.New("boom")
	case "tar":
		return io.NopCloser(strings.NewReader(strings.Repeat("x", 1024))), nil
	}
	return b.Layer.Uncompressed()
}

func (b brokenLayer) MediaType() (types.MediaType, error) { return b.Layer.MediaType() }

func TestIndexErrors(t *testing.T) {
	img := testimg.Build(testimg.Layer("FROM scratch", testimg.File{Path: "a", Body: "a"}))
	for _, stage := range []string{"config", "digest", "layers", "diffid", "ldigest", "size", "open", "tar"} {
		if _, err := (Indexer{}).Index(context.Background(), open(t, broken{img, stage})); err == nil {
			t.Errorf("stage %s: no error", stage)
		}
	}
	if short("sha256:0123456789abcdef") != "0123456789ab" || short("sha256:ab") != "ab" {
		t.Error("short")
	}
}
