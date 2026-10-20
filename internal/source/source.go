// Package source turns an image reference into a v1.Image, from the local
// Docker daemon, a registry, a `docker save` tarball or an OCI layout.
package source

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// Kind is where an image is read from.
type Kind string

// The image sources. Auto picks one from the reference.
const (
	Auto   Kind = "auto"
	Docker Kind = "docker"
	Remote Kind = "remote"
	Tar    Kind = "tar"
	OCI    Kind = "oci"
)

// Kinds lists the valid values of --source, in help order.
var Kinds = []Kind{Auto, Docker, Remote, Tar, OCI}

// ParseKind validates a --source value.
func ParseKind(s string) (Kind, error) {
	for _, k := range Kinds {
		if string(k) == s {
			return k, nil
		}
	}
	return "", fmt.Errorf("invalid source %q (use one of auto, docker, remote, tar, oci)", s)
}

// ParsePlatform parses "os/arch[/variant]".
func ParsePlatform(s string) (*v1.Platform, error) {
	if s == "" {
		return nil, nil
	}
	p, err := v1.ParsePlatform(s)
	if err != nil || p.OS == "" || p.Architecture == "" {
		return nil, fmt.Errorf("invalid platform %q (use os/arch[/variant], like linux/arm64)", s)
	}
	return p, nil
}

// ErrNotFound and ErrAuth classify failures so callers can pick exit codes.
var (
	ErrNotFound = errors.New("image not found")
	ErrAuth     = errors.New("registry authentication failed")
)

// Image is an opened image and where it came from.
type Image struct {
	Ref      string
	Kind     Kind
	Platform string
	v1.Image
}

// Options control how references resolve.
type Options struct {
	Kind     Kind
	Platform *v1.Platform // nil: linux on the host's architecture
	Keychain authn.Keychain
	// Daemon opens an image from the Docker daemon; nil uses daemon.Image.
	Daemon func(name.Reference, ...daemon.Option) (v1.Image, error)
	// Transport is the registry HTTP transport; nil uses the default.
	Transport http.RoundTripper
}

func (o Options) platform() v1.Platform {
	if o.Platform != nil {
		return *o.Platform
	}
	return v1.Platform{OS: "linux", Architecture: runtime.GOARCH}
}

// Open resolves ref and opens the image it names.
func Open(ctx context.Context, ref string, o Options) (*Image, error) {
	kind, target := classify(ref, o.Kind)
	var (
		img v1.Image
		err error
	)
	switch kind {
	case Tar:
		img, err = openTar(target)
	case OCI:
		img, err = openOCI(target, o)
	case Docker:
		img, err = openDaemon(ctx, target, o)
	case Remote:
		img, err = openRemote(ctx, target, o)
	default: // Auto, by elimination: a local image, else the registry.
		img, err = openDaemon(ctx, target, o)
		kind = Docker
		if err != nil {
			if errors.Is(err, errBadRef) {
				return nil, err
			}
			img, err = openRemote(ctx, target, o)
			kind = Remote
			if errors.Is(err, ErrNotFound) {
				err = fmt.Errorf("%w: %s is not in the local Docker daemon or its registry", ErrNotFound, ref)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("read image config for %s: %w", ref, classifyRemote(ref, err))
	}
	p := cfg.OS + "/" + cfg.Architecture
	if cfg.Variant != "" {
		p += "/" + cfg.Variant
	}
	return &Image{Ref: ref, Kind: kind, Platform: p, Image: img}, nil
}

// classify picks the source for ref: an explicit scheme, then --source,
// then an existing path (directory: OCI layout, file: tarball), else Auto.
func classify(ref string, forced Kind) (Kind, string) {
	for _, s := range []struct {
		prefix string
		kind   Kind
	}{{"tar://", Tar}, {"oci://", OCI}, {"registry://", Remote}, {"docker://", Docker}} {
		if rest, ok := strings.CutPrefix(ref, s.prefix); ok {
			return s.kind, rest
		}
	}
	if forced != "" && forced != Auto {
		return forced, ref
	}
	if fi, err := os.Stat(ref); err == nil {
		if fi.IsDir() {
			return OCI, ref
		}
		return Tar, ref
	}
	if looksLikePath(ref) {
		return Tar, ref // a missing file: say so, rather than asking a registry
	}
	return Auto, ref
}

// looksLikePath reports whether ref can only be a file: image references
// never start with / or . and a name ending in .tar has no tag.
func looksLikePath(ref string) bool {
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, ".") || strings.HasPrefix(ref, "~") {
		return true
	}
	last := ref[strings.LastIndex(ref, "/")+1:]
	return (strings.HasSuffix(ref, ".tar") || strings.HasSuffix(ref, ".tar.gz")) && !strings.Contains(last, ":")
}

var errBadRef = errors.New("invalid image reference")

func parseRef(ref string) (name.Reference, error) {
	r, err := name.ParseReference(ref)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %v", errBadRef, ref, err)
	}
	return r, nil
}

func openTar(path string) (v1.Image, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%w: no tarball at %s", ErrNotFound, path)
	}
	img, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		return nil, fmt.Errorf("read tarball %s: %w (it must hold exactly one image, as written by `docker save` or buildx type=docker)", path, err)
	}
	return img, nil
}

func openOCI(target string, o Options) (v1.Image, error) {
	dir, tag := target, ""
	if i := strings.LastIndex(target, ":"); i > 0 && !strings.Contains(target[i:], string(filepath.Separator)) {
		if _, err := os.Stat(target); err != nil {
			dir, tag = target[:i], target[i+1:]
		}
	}
	idx, err := layout.ImageIndexFromPath(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: no OCI layout at %s (%v)", ErrNotFound, dir, err)
	}
	return pick(idx, o.platform(), tag, dir)
}

// pick chooses one image from an index: by ref-name annotation when tag is
// set, else by platform, else the only image there is.
func pick(idx v1.ImageIndex, p v1.Platform, tag, where string) (v1.Image, error) {
	im, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("read index in %s: %w", where, err)
	}
	var images []v1.Descriptor
	for _, d := range im.Manifests {
		if tag != "" && d.Annotations["org.opencontainers.image.ref.name"] != tag {
			continue
		}
		if d.MediaType.IsIndex() {
			child, err := idx.ImageIndex(d.Digest)
			if err != nil {
				return nil, fmt.Errorf("read nested index in %s: %w", where, err)
			}
			return pick(child, p, "", where)
		}
		if d.MediaType.IsImage() {
			if d.Platform != nil && d.Platform.OS == "unknown" {
				continue // attestation manifests
			}
			images = append(images, d)
		}
	}
	for _, d := range images {
		if d.Platform != nil && d.Platform.Satisfies(p) {
			return idx.Image(d.Digest)
		}
	}
	switch {
	case len(images) == 1:
		return idx.Image(images[0].Digest)
	case len(images) == 0 && tag != "":
		return nil, fmt.Errorf("%w: no image tagged %q in %s", ErrNotFound, tag, where)
	case len(images) == 0:
		return nil, fmt.Errorf("%w: no images in %s", ErrNotFound, where)
	}
	return nil, fmt.Errorf("%w: no %s image in %s; choose one with --platform (it has %s)", ErrNotFound, platformString(p), where, platforms(images))
}

func platformString(p v1.Platform) string {
	s := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		s += "/" + p.Variant
	}
	return s
}

func platforms(ds []v1.Descriptor) string {
	var out []string
	for _, d := range ds {
		if d.Platform != nil {
			out = append(out, platformString(*d.Platform))
		}
	}
	return strings.Join(out, ", ")
}

func openDaemon(ctx context.Context, ref string, o Options) (v1.Image, error) {
	r, err := parseRef(ref)
	if err != nil {
		return nil, err
	}
	open := o.Daemon
	if open == nil {
		open = daemon.Image
	}
	img, err := open(r, daemon.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not in the local Docker daemon (%v)", ErrNotFound, ref, err)
	}
	return img, nil
}

func openRemote(ctx context.Context, ref string, o Options) (v1.Image, error) {
	r, err := parseRef(ref)
	if err != nil {
		return nil, err
	}
	kc := o.Keychain
	if kc == nil {
		kc = authn.DefaultKeychain
	}
	opts := []remote.Option{remote.WithContext(ctx), remote.WithAuthFromKeychain(kc), remote.WithPlatform(o.platform())}
	if o.Transport != nil {
		opts = append(opts, remote.WithTransport(o.Transport))
	}
	img, err := remote.Image(r, opts...)
	if err != nil {
		return nil, classifyRemote(ref, err)
	}
	return img, nil
}

// classifyRemote wraps registry errors in ErrAuth or ErrNotFound.
func classifyRemote(ref string, err error) error {
	var te *transport.Error
	if errors.As(err, &te) {
		switch te.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%w for %s: %v", ErrAuth, ref, err)
		case http.StatusNotFound:
			return fmt.Errorf("%w: %s (%v)", ErrNotFound, ref, err)
		}
		for _, d := range te.Errors {
			if d.Code == transport.ManifestUnknownErrorCode || d.Code == transport.NameUnknownErrorCode {
				return fmt.Errorf("%w: %s (%v)", ErrNotFound, ref, err)
			}
		}
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrAuth) {
		return err
	}
	return fmt.Errorf("open %s: %w", ref, err)
}
