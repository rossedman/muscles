package source

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/rossedman/muscles/internal/testimg"
)

func img(arch string) v1.Image {
	return testimg.BuildFor(v1.Platform{OS: "linux", Architecture: arch}, testimg.Layer("FROM scratch", testimg.File{Path: "a", Body: arch}))
}

func noDaemon(name.Reference, ...daemon.Option) (v1.Image, error) {
	return nil, errors.New("Cannot connect to the Docker daemon")
}

func TestParseKindAndPlatform(t *testing.T) {
	for _, k := range Kinds {
		if got, err := ParseKind(string(k)); err != nil || got != k {
			t.Fatalf("ParseKind(%s) = %v %v", k, got, err)
		}
	}
	if _, err := ParseKind("podman"); err == nil {
		t.Fatal("ParseKind accepted podman")
	}
	if p, err := ParsePlatform(""); p != nil || err != nil {
		t.Fatal("empty platform")
	}
	if p, err := ParsePlatform("linux/arm64/v8"); err != nil || p.Variant != "v8" {
		t.Fatalf("platform %v %v", p, err)
	}
	if _, err := ParsePlatform("linux"); err == nil {
		t.Fatal("ParsePlatform accepted linux")
	}
}

func TestTar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.tar")
	tag, _ := name.NewTag("app:dev")
	if err := tarball.WriteToFile(path, tag, img("amd64")); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{path, "tar://" + path} {
		got, err := Open(context.Background(), ref, Options{})
		if err != nil || got.Kind != Tar || got.Platform != "linux/amd64" {
			t.Fatalf("Open(%s) = %+v, %v", ref, got, err)
		}
	}
	if _, err := Open(context.Background(), "tar://"+filepath.Join(dir, "missing.tar"), Options{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing tar: %v", err)
	}
	bad := filepath.Join(dir, "bad.tar")
	os.WriteFile(bad, []byte("not a tar"), 0o644)
	if _, err := Open(context.Background(), bad, Options{}); err == nil || !strings.Contains(err.Error(), "exactly one image") {
		t.Fatalf("bad tar: %v", err)
	}
}

func writeLayout(t *testing.T, dir string, idx v1.ImageIndex) {
	t.Helper()
	if _, err := layout.Write(dir, idx); err != nil {
		t.Fatal(err)
	}
}

func TestOCI(t *testing.T) {
	dir := t.TempDir()
	amd, arm := img("amd64"), img("arm64")
	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: amd, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}, Annotations: map[string]string{"org.opencontainers.image.ref.name": "v1"}}},
		mutate.IndexAddendum{Add: arm, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
		mutate.IndexAddendum{Add: img("amd64"), Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "unknown", Architecture: "unknown"}}},
	)
	writeLayout(t, dir, idx)

	got, err := Open(context.Background(), dir, Options{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}})
	if err != nil || got.Kind != OCI || got.Platform != "linux/arm64" {
		t.Fatalf("by platform: %+v %v", got, err)
	}
	got, err = Open(context.Background(), "oci://"+dir+":v1", Options{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}})
	if err != nil || got.Platform != "linux/amd64" {
		t.Fatalf("by tag: %+v %v", got, err)
	}
	if _, err := Open(context.Background(), "oci://"+dir+":nope", Options{}); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), `no image tagged "nope"`) {
		t.Fatalf("missing tag: %v", err)
	}
	if _, err := Open(context.Background(), dir, Options{Platform: &v1.Platform{OS: "linux", Architecture: "s390x"}}); err == nil || !strings.Contains(err.Error(), "linux/amd64, linux/arm64") {
		t.Fatalf("no platform: %v", err)
	}
	if _, err := Open(context.Background(), "oci://"+filepath.Join(dir, "nope"), Options{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing layout: %v", err)
	}

	// Nested index, as buildx writes, and a single image without platform.
	nested := t.TempDir()
	writeLayout(t, nested, mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: mutate.AppendManifests(empty.Index, mutate.IndexAddendum{Add: amd})}))
	if got, err := Open(context.Background(), nested, Options{Kind: OCI}); err != nil || got.Platform != "linux/amd64" {
		t.Fatalf("nested: %+v %v", got, err)
	}
	emptyDir := t.TempDir()
	writeLayout(t, emptyDir, empty.Index)
	if _, err := Open(context.Background(), emptyDir, Options{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty: %v", err)
	}
}

func TestDaemon(t *testing.T) {
	want := img(runtime.GOARCH)
	opts := Options{Daemon: func(r name.Reference, _ ...daemon.Option) (v1.Image, error) {
		if r.Name() != "index.docker.io/library/app:dev" {
			return nil, errors.New("No such image")
		}
		return want, nil
	}}
	got, err := Open(context.Background(), "app:dev", opts)
	if err != nil || got.Kind != Docker {
		t.Fatalf("auto daemon: %+v %v", got, err)
	}
	if got, err := Open(context.Background(), "docker://app:dev", opts); err != nil || got.Kind != Docker {
		t.Fatalf("docker://: %+v %v", got, err)
	}
	opts.Kind = Docker
	if _, err := Open(context.Background(), "other:dev", opts); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := Open(context.Background(), "Bad Ref!", Options{Daemon: noDaemon}); err == nil || !strings.Contains(err.Error(), "invalid image reference") {
		t.Fatalf("bad ref: %v", err)
	}
	if _, err := Open(context.Background(), "Bad Ref!", Options{Kind: Remote}); err == nil || !strings.Contains(err.Error(), "invalid image reference") {
		t.Fatalf("bad remote ref: %v", err)
	}
}

func TestRemote(t *testing.T) {
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	ref := host + "/acme/app:1.0"
	r, _ := name.ParseReference(ref)
	idx := mutate.AppendManifests(empty.Index,
		mutate.IndexAddendum{Add: img("amd64"), Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "amd64"}}},
		mutate.IndexAddendum{Add: img("arm64"), Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}}},
	)
	if err := remote.WriteIndex(r, idx); err != nil {
		t.Fatal(err)
	}

	got, err := Open(context.Background(), ref, Options{Daemon: noDaemon, Keychain: authn.NewMultiKeychain(), Platform: &v1.Platform{OS: "linux", Architecture: "arm64"}})
	if err != nil || got.Kind != Remote || got.Platform != "linux/arm64" {
		t.Fatalf("auto remote: %+v %v", got, err)
	}
	if got, err := Open(context.Background(), "registry://"+ref, Options{Transport: http.DefaultTransport}); err != nil || got.Platform != "linux/"+runtime.GOARCH {
		if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
			t.Fatalf("registry://: %+v %v", got, err)
		}
	}
	if _, err := Open(context.Background(), host+"/acme/missing:1", Options{Daemon: noDaemon}); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "local Docker daemon or its registry") {
		t.Fatalf("missing: %v", err)
	}
}

func TestRemoteAuthAndErrors(t *testing.T) {
	status := http.StatusUnauthorized
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	ref := strings.TrimPrefix(srv.URL, "http://") + "/acme/app:1"
	if _, err := Open(context.Background(), ref, Options{Kind: Remote}); !errors.Is(err, ErrAuth) {
		t.Fatalf("401: %v", err)
	}
	status = http.StatusInternalServerError
	if _, err := Open(context.Background(), ref, Options{Kind: Remote}); err == nil || errors.Is(err, ErrAuth) || errors.Is(err, ErrNotFound) {
		t.Fatalf("500: %v", err)
	}
}

func TestClassifyRemote(t *testing.T) {
	cases := []struct {
		err  error
		want error
	}{
		{&transport.Error{StatusCode: http.StatusForbidden}, ErrAuth},
		{&transport.Error{StatusCode: http.StatusNotFound}, ErrNotFound},
		{&transport.Error{StatusCode: http.StatusBadRequest, Errors: []transport.Diagnostic{{Code: transport.NameUnknownErrorCode}}}, ErrNotFound},
		{ErrAuth, ErrAuth},
	}
	for _, c := range cases {
		if got := classifyRemote("x", c.err); !errors.Is(got, c.want) {
			t.Errorf("classifyRemote(%v) = %v", c.err, got)
		}
	}
}

type badConfig struct{ v1.Image }

func (badConfig) ConfigFile() (*v1.ConfigFile, error) { return nil, errors.New("boom") }

func TestConfigError(t *testing.T) {
	_, err := Open(context.Background(), "app:dev", Options{Daemon: func(name.Reference, ...daemon.Option) (v1.Image, error) { return badConfig{img("amd64")}, nil }})
	if err == nil || !strings.Contains(err.Error(), "read image config") {
		t.Fatalf("config error: %v", err)
	}
}
