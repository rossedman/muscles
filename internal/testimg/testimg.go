// Package testimg builds small container images in memory for tests: each
// step is one layer (or an empty history entry), each file a tar header.
package testimg

import (
	"archive/tar"
	"bytes"
	"io"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// File is one tar entry. The zero Type is a regular file.
type File struct {
	Path string
	Body string
	Size int64 // when > 0 and Body is empty, the body is Size zero bytes
	Mode int64
	UID  int
	GID  int
	Type byte
	Link string
}

// Dir returns a directory entry.
func Dir(path string) File { return File{Path: path, Type: tar.TypeDir, Mode: 0o755} }

// Whiteout returns the marker that deletes path in lower layers.
func Whiteout(path string) File {
	dir, base := split(path)
	return File{Path: dir + ".wh." + base}
}

// Opaque returns the marker that hides everything below dir in lower layers.
func Opaque(dir string) File { return File{Path: dir + "/.wh..wh..opq"} }

// split separates a path into its directory, with trailing slash, and its base name.
func split(p string) (string, string) {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i+1], p[i+1:]
		}
	}
	return "", p
}

// Step is one Dockerfile instruction. Empty steps (ENV, LABEL, ...) record
// history without a layer.
type Step struct {
	CreatedBy string
	Empty     bool
	Files     []File
}

// Layer returns a step that produces a layer.
func Layer(createdBy string, files ...File) Step { return Step{CreatedBy: createdBy, Files: files} }

// Empty returns a step without a layer.
func Empty(createdBy string) Step { return Step{CreatedBy: createdBy, Empty: true} }

// TarBytes renders files as an uncompressed tar stream.
func TarBytes(files ...File) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range files {
		typ := f.Type
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		body := []byte(f.Body)
		if len(body) == 0 && f.Size > 0 {
			body = make([]byte, f.Size)
		}
		h := &tar.Header{Name: f.Path, Typeflag: typ, Mode: mode, Uid: f.UID, Gid: f.GID, Linkname: f.Link, ModTime: time.Unix(0, 0)}
		if typ == tar.TypeReg {
			h.Size = int64(len(body))
		}
		if err := tw.WriteHeader(h); err != nil {
			panic(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write(body); err != nil {
				panic(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// LayerOf returns a gzip-compressed layer holding files.
func LayerOf(files ...File) v1.Layer {
	b := TarBytes(files...)
	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil })
	if err != nil {
		panic(err)
	}
	return l
}

// Build returns a linux/amd64 image made of steps, with matching history.
func Build(steps ...Step) v1.Image {
	return BuildFor(v1.Platform{OS: "linux", Architecture: "amd64"}, steps...)
}

// BuildFor returns an image for platform made of steps.
func BuildFor(p v1.Platform, steps ...Step) v1.Image {
	img := empty.Image
	var history []v1.History
	created := time.Date(2026, 10, 18, 14, 2, 0, 0, time.UTC)
	for i, s := range steps {
		history = append(history, v1.History{CreatedBy: s.CreatedBy, EmptyLayer: s.Empty, Created: v1.Time{Time: created.Add(time.Duration(i) * time.Minute)}})
		if s.Empty {
			continue
		}
		var err error
		img, err = mutate.AppendLayers(img, LayerOf(s.Files...))
		if err != nil {
			panic(err)
		}
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		panic(err)
	}
	cfg = cfg.DeepCopy()
	cfg.History = history
	cfg.OS, cfg.Architecture, cfg.Variant = p.OS, p.Architecture, p.Variant
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		panic(err)
	}
	return img
}
