// Package index reads each layer of an image once, as a tar stream, and
// records one Entry per tar header. Contents are hashed, never kept. Layer
// indexes are content-addressed by diffID, so they can be cached forever.
package index

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"

	"github.com/rossedman/muscles/internal/pkgdb"
	"github.com/rossedman/muscles/internal/source"
)

// Type is the kind of a tar entry.
type Type uint8

// Entry types. Whiteout deletes Path from lower layers; Opaque hides
// everything below the directory Path in lower layers.
const (
	File Type = iota
	Dir
	Symlink
	Hardlink
	Other
	Whiteout
	Opaque
)

func (t Type) String() string {
	return [...]string{"file", "dir", "symlink", "hardlink", "other", "whiteout", "opaque"}[t]
}

// Entry is one tar header, with a hash of the contents for regular files.
type Entry struct {
	Path string // absolute and clean: "/usr/bin/curl"
	Type Type
	Size int64
	Mode uint32 // permission and special bits
	UID  int
	GID  int
	Link string
	Hash uint64 // xxhash64 of the contents (regular files)
}

// Layer is the index of one layer.
type Layer struct {
	DiffID     string
	Digest     string // compressed digest, as the source reports it
	Compressed int64  // compressed size, as the source reports it
	Size       int64  // sum of the regular files' sizes
	Entries    []Entry
	// Packages are the package database files in this layer (dpkg, apk)
	// and Python .dist-info metadata, keyed by path, as their packages.
	Packages map[string][]pkgdb.Pkg
}

// Step is one build step from the image config's history.
type Step struct {
	CreatedBy string
	Created   time.Time
	Empty     bool
}

// Image is an indexed image.
type Image struct {
	Ref        string
	Source     source.Kind
	Platform   string
	Digest     string
	Compressed int64
	Layers     []*Layer
	History    []Step
}

// Progress reports indexing of one layer.
type Progress struct {
	Index, Total int
	DiffID       string
	Compressed   int64
	Cached       bool
	Done         bool
	Elapsed      time.Duration
}

// Indexer indexes images, optionally through a cache.
type Indexer struct {
	Cache    *Cache // nil: no cache
	Fresh    bool   // ignore cached layers (still writes them)
	Progress func(Progress)
	Now      func() time.Time
}

// Index reads every layer of img.
func (ix Indexer) Index(ctx context.Context, img *source.Image) (*Image, error) {
	now := ix.Now
	if now == nil {
		now = time.Now
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("read image config: %w", err)
	}
	digest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("read image digest: %w", err)
	}
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("list layers: %w", err)
	}
	out := &Image{Ref: img.Ref, Source: img.Kind, Platform: img.Platform, Digest: digest.String()}
	for _, h := range cfg.History {
		out.History = append(out.History, Step{CreatedBy: h.CreatedBy, Created: h.Created.Time, Empty: h.EmptyLayer})
	}
	for i, l := range layers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		start := now()
		diffID, err := l.DiffID()
		if err != nil {
			return nil, fmt.Errorf("layer %d: read diffID: %w", i, err)
		}
		var li *Layer
		cached := false
		if ix.Cache != nil && !ix.Fresh {
			li, cached = ix.Cache.Get(diffID.String())
		}
		// A registry's manifest knows each layer's digest and size. Tarballs
		// and the daemon may hold uncompressed layers, which go-containerregistry
		// compresses to answer; for those, reuse what the cache recorded.
		if !cached || img.Kind == source.Remote || li.Digest == "" {
			d, err := l.Digest()
			if err != nil {
				return nil, fmt.Errorf("layer %d: read digest: %w", i, err)
			}
			compressed, err := l.Size()
			if err != nil {
				return nil, fmt.Errorf("layer %d: read size: %w", i, err)
			}
			if li == nil {
				li = &Layer{DiffID: diffID.String()}
			}
			li.Digest, li.Compressed = d.String(), compressed
		}
		p := Progress{Index: i, Total: len(layers), DiffID: diffID.String(), Compressed: li.Compressed, Cached: cached}
		if !cached {
			ix.report(p)
			rc, err := l.Uncompressed()
			if err != nil {
				return nil, fmt.Errorf("layer %d: open: %w", i, err)
			}
			entries, size, pkgs, err := ReadTar(ctx, rc)
			_ = rc.Close()
			if err != nil {
				return nil, fmt.Errorf("layer %d (%s): %w", i, short(diffID.String()), err)
			}
			li.Entries, li.Size, li.Packages = entries, size, pkgs
			if ix.Cache != nil {
				// A cache that can't be written only costs speed.
				_ = ix.Cache.Put(li)
			}
		}
		compressed := li.Compressed
		out.Layers = append(out.Layers, li)
		out.Compressed += compressed
		p.Done, p.Elapsed = true, now().Sub(start)
		ix.report(p)
	}
	return out, nil
}

func (ix Indexer) report(p Progress) {
	if ix.Progress != nil {
		ix.Progress(p)
	}
}

func short(d string) string {
	_, hex, _ := strings.Cut(d, ":")
	if len(hex) > 12 {
		return hex[:12]
	}
	return hex
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// ReadTar indexes an uncompressed layer tar. It returns the entries, the
// total size of the regular files, and the packages declared by the package
// databases in it (see pkgdb.IsDB), keyed by path.
func ReadTar(ctx context.Context, r io.Reader) ([]Entry, int64, map[string][]pkgdb.Pkg, error) {
	tr := tar.NewReader(ctxReader{ctx, r})
	var (
		entries []Entry
		total   int64
		buf     = make([]byte, 64<<10)
		pkgs    map[string][]pkgdb.Pkg
	)
	addPkgs := func(p string, ps []pkgdb.Pkg) {
		if pkgs == nil {
			pkgs = map[string][]pkgdb.Pkg{}
		}
		pkgs[p] = ps
	}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries, total, pkgs, nil
		}
		if err != nil {
			return nil, 0, nil, fmt.Errorf("read tar: %w", err)
		}
		p := Clean(h.Name)
		if p == "/" {
			continue
		}
		e := Entry{Path: p, Mode: uint32(h.Mode & 0o7777), UID: h.Uid, GID: h.Gid}
		dir, base := path.Split(p)
		if marker, ok := strings.CutPrefix(base, ".wh."); ok {
			switch {
			case marker == ".wh..opq":
				e = Entry{Path: Clean(dir), Type: Opaque}
			case strings.HasPrefix(marker, ".wh."):
				continue // aufs metadata, not a deletion
			default:
				e = Entry{Path: Clean(dir + marker), Type: Whiteout}
			}
			entries = append(entries, e)
			continue
		}
		switch h.Typeflag {
		case tar.TypeReg:
			e.Type, e.Size = File, h.Size
			d := xxhash.New()
			var src io.Reader = tr
			var db *bytes.Buffer
			if pkgdb.IsDB(p) && h.Size <= pkgdb.MaxDB {
				db = &bytes.Buffer{}
				src = io.TeeReader(tr, db)
			}
			if _, err := io.CopyBuffer(d, src, buf); err != nil {
				return nil, 0, nil, fmt.Errorf("read %s: %w", p, err)
			}
			e.Hash = d.Sum64()
			total += h.Size
			if db != nil {
				addPkgs(p, pkgdb.Parse(p, db.Bytes()))
			} else if pkg, ok := pkgdb.FromPath(p); ok {
				addPkgs(p, []pkgdb.Pkg{pkg})
			}
		case tar.TypeDir:
			e.Type = Dir
		case tar.TypeSymlink:
			e.Type, e.Link = Symlink, h.Linkname
		case tar.TypeLink:
			e.Type, e.Link = Hardlink, Clean(h.Linkname)
		default:
			e.Type = Other
		}
		entries = append(entries, e)
	}
}

// Clean makes a tar name absolute and clean: "./usr//bin/" is "/usr/bin".
func Clean(name string) string {
	return path.Clean("/" + strings.TrimPrefix(name, "./"))
}
