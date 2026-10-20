package testimg

import (
	"os"
	"path/filepath"
	"strings"
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
		{"app-1.8.0.tar", "ghcr.io/acme/app:1.8.0", Build(withPackages(AppSteps(unit), "libssl3 3.0.14-1", "numpy 2.0.2")...)},
		{"app-1.9.0.tar", "ghcr.io/acme/app:1.9.0", Build(withPackages(AppNextSteps(unit), "libssl3 3.0.15-1", "")...)},
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

// withPackages adds package databases to the app's steps, so the demo
// shows what its apt and pip layers installed: base packages in L0, curl
// and libssl3 in L1, pandas, six and (if not empty) numpy in L3.
func withPackages(steps []Step, libssl, numpy string) []Step {
	dpkg := func(pkgs ...string) string {
		var b strings.Builder
		for _, p := range pkgs {
			n, v, _ := strings.Cut(p, " ")
			b.WriteString("Package: " + n + "\nStatus: install ok installed\nVersion: " + v + "\n\n")
		}
		return b.String()
	}
	pip := func(p string) File {
		n, v, _ := strings.Cut(p, " ")
		return File{Path: "usr/local/lib/python3.12/site-packages/" + n + "-" + v + ".dist-info/METADATA", Body: "Metadata-Version: 2.1\nName: " + n + "\nVersion: " + v + "\n"}
	}
	base := []string{"base-files 12.4", "bash 5.2.15-2", "libc6 2.36-9"}
	steps[0].Files = append(steps[0].Files, File{Path: "var/lib/dpkg/status", Body: dpkg(base...)})
	steps[2].Files = append(steps[2].Files, File{Path: "var/lib/dpkg/status", Body: dpkg(append(base, "curl 7.88.1-10", libssl)...)})
	steps[4].Files = append(steps[4].Files, pip("pandas 2.2.3"), pip("six 1.16.0"))
	if numpy != "" {
		steps[4].Files = append(steps[4].Files, pip(numpy))
	}
	return steps
}

func TestApp(t *testing.T) {
	for _, img := range []v1.Image{App(1), AppNext(1)} {
		if ls, err := img.Layers(); err != nil || len(ls) != 6 {
			t.Fatalf("layers: %d %v", len(ls), err)
		}
	}
}
