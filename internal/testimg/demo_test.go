package testimg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// TestWriteDemo writes the demo images used in the docs when
// MUSCLES_DEMO_DIR is set:
//
//	MUSCLES_DEMO_DIR=/tmp/demo go test ./internal/testimg -run TestWriteDemo
func TestWriteDemo(t *testing.T) {
	dir := os.Getenv("MUSCLES_DEMO_DIR")
	if dir == "" {
		t.Skip("MUSCLES_DEMO_DIR not set")
	}
	const unit = 100_000 // sizes in tenths of a megabyte
	for _, d := range []struct {
		file, tag string
		img       v1.Image
	}{
		{"app-1.8.0.tar", "ghcr.io/acme/app:1.8.0", App(unit)},
		{"app-1.9.0.tar", "ghcr.io/acme/app:1.9.0", AppNext(unit)},
	} {
		tag, err := name.NewTag(d.tag)
		if err != nil {
			t.Fatal(err)
		}
		if err := tarball.WriteToFile(filepath.Join(dir, d.file), tag, d.img); err != nil {
			t.Fatal(err)
		}
	}
}

func TestApp(t *testing.T) {
	for _, img := range []v1.Image{App(1), AppNext(1)} {
		if ls, err := img.Layers(); err != nil || len(ls) != 6 {
			t.Fatalf("layers: %d %v", len(ls), err)
		}
	}
}
