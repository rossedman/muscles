package testimg

import (
	"archive/tar"
	"bytes"
	"io"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func TestBuild(t *testing.T) {
	img := BuildFor(v1.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"},
		Layer("FROM scratch", Dir("etc"), File{Path: "etc/os-release", Body: "x"}, File{Path: "big", Size: 10}),
		Empty("ENV A=1"),
		Layer("RUN rm", Whiteout("etc/os-release"), Opaque("var"), File{Path: "bin/sh", Type: tar.TypeSymlink, Link: "/bin/busybox"}),
	)
	layers, err := img.Layers()
	if err != nil || len(layers) != 2 {
		t.Fatalf("layers: %d %v", len(layers), err)
	}
	cfg, _ := img.ConfigFile()
	if len(cfg.History) != 3 || !cfg.History[1].EmptyLayer || cfg.Architecture != "arm64" || cfg.Variant != "v8" {
		t.Fatalf("config: %+v", cfg)
	}
	rc, err := layers[1].Uncompressed()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	tr := tar.NewReader(bytes.NewReader(b))
	var names []string
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		names = append(names, h.Name)
	}
	want := []string{"etc/.wh.os-release", "var/.wh..wh..opq", "bin/sh"}
	if len(names) != len(want) {
		t.Fatalf("names %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names %v", names)
		}
	}
	if d, b := split("noslash"); d != "" || b != "noslash" {
		t.Fatalf("split: %q %q", d, b)
	}
}
