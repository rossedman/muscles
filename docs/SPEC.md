# muscles — Specification

> A terminal UI for exploring container image layers and seeing exactly
> **what changed in each one**.

| | |
|---|---|
| **Status** | Draft v0.2 (replaces the v0.1 "engineering life" spec) |
| **Author** | Ross Edman |
| **Last updated** | 2026-10-20 |
| **Language** | Go (1.23+) |
| **Libraries** | Cobra, Viper, Bubble Tea, Bubbles, Lip Gloss, go-containerregistry |

---

## 1. Problem

A container image is a stack of filesystem diffs, but the tools for looking at
it show you very little. `docker history` gives you a list of commands and
sizes. To learn which files a layer actually added, changed or deleted, you
have to export the image, untar every layer and diff them by hand.

That leaves everyday questions hard to answer:

- Why is this image 1.4 GB? Which layer, and which files?
- What did `RUN apt-get install ...` actually put on disk?
- Did my cleanup step really delete the cache, or is it still in an earlier
  layer?
- Why did `chown -R` double the size of my app directory?
- What changed between `myapp:1.8.0` and `myapp:1.9.0`?

## 2. Goals

1. **Per-layer changes.** For any layer, show the files it **added**,
   **modified** and **deleted**, with sizes, next to the Dockerfile
   instruction that created it.
2. **A plain summary of each layer.** One line saying what the layer did, e.g.
   *"+1,204 files, 182 MB, mostly /usr/lib/python3.12; deleted
   /var/lib/apt/lists (12 MB still stored in layer 3)"*.
3. **Find waste.** Point out bytes that are stored but not visible in the final
   image: files that were overwritten or deleted later, and files that were
   copied again only because their permissions or owner changed.
4. **Compare two images.** Show which layers they share and what differs in
   the final filesystem, so you can see what a release changed.
5. **Fast and works anywhere.** Read images from the local Docker daemon, a
   registry (without pulling), a `docker save` tarball or an OCI layout
   directory. Re-opening an image you've already explored is instant.
6. **Scriptable.** Every view has a non-interactive command with `--json`
   output.

## 3. Non-goals

- Building, editing or pushing images.
- Vulnerability scanning (use Trivy or Grype).
- Running containers or inspecting running containers.
- Windows container images (Linux images only in v1).

## 4. Prior art

[dive](https://github.com/wagoodman/dive) is the closest existing tool. It
shows a file tree per layer and an image "efficiency" score. `muscles` should
not just be a rewrite of dive. It differs in these ways:

| | dive | muscles |
|---|---|---|
| Per-layer file tree | ✓ | ✓ |
| Plain-language summary per layer | — | ✓ |
| Open a registry image without pulling it | — | ✓ |
| Detect metadata-only rewrites (`chmod`/`chown` copies) | — | ✓ |
| Show a file's previous version and a text diff | — | ✓ |
| Compare two images | — | ✓ |
| JSON output for every view | partial | ✓ |
| Cache of parsed layers by digest | — | ✓ |

## 5. Core scenarios

| # | Scenario | What `muscles` does |
|---|----------|---------------------|
| S1 | "Why is this image so big?" | `muscles myapp:latest` opens with layers sorted top-down, sizes, and a **Waste** figure. One key jumps to the biggest wasted files and the layers responsible. |
| S2 | "What did this RUN step install?" | Select the layer: the right pane shows only that layer's changes as a tree, with the summary line above it. |
| S3 | "Did my cleanup work?" | Deleted paths show in red with a note saying which earlier layer still holds the bytes. |
| S4 | "What changed in this release?" | `muscles diff myapp:1.8.0 myapp:1.9.0` shows shared base layers, the layer where they diverge, and a file-level diff of the final filesystems. |
| S5 | "Fail CI if the image regresses." | `muscles check myapp:sha --max-waste 50MB --max-size 800MB` exits non-zero with a short report. (v1.1) |

## 6. How it works

### 6.1 Reading images

Use [`go-containerregistry`](https://github.com/google/go-containerregistry)
(`ggcr`). It gives one `v1.Image` interface over every source:

| Source | Reference form | ggcr package |
|--------|----------------|--------------|
| Docker / Podman daemon | `myapp:latest` (default when the image exists locally) | `pkg/v1/daemon` |
| Registry | `registry://ghcr.io/org/app:tag`, or any reference not found locally | `pkg/v1/remote` |
| `docker save` tarball | `tar://./app.tar` or a path ending in `.tar` | `pkg/v1/tarball` |
| OCI layout directory | `oci://./layout[:tag]` | `pkg/v1/layout` |

- `--source docker|remote|tar|oci` forces a source; otherwise it's detected.
- Registry auth reuses `~/.docker/config.json` and credential helpers
  (`authn.DefaultKeychain`).
- Multi-platform images: pick the platform matching the host by default;
  `--platform linux/arm64` overrides it, and the TUI lists the others.
- Remote layers stream one at a time and are never written to disk in full.

### 6.2 Building the change index

Each layer is a tar stream. `muscles` reads every layer **once**, in order,
and records one entry per tar header:

```go
type Entry struct {
    Path     string
    Type     EntryType   // file, dir, symlink, hardlink, other
    Size     int64
    Mode     fs.FileMode
    UID, GID int
    LinkName string
    Hash     uint64      // xxhash of contents, for regular files
}
```

Changes are classified against the filesystem built from all lower layers:

| Change | Rule |
|--------|------|
| **Added** | The path doesn't exist in the lower filesystem. |
| **Modified** | The path exists below and the content hash or type differs. |
| **Metadata only** | The path exists below with the same content; only mode, owner or mtime differ. The full file is still stored again, so this counts as waste. |
| **Deleted** | A whiteout `.wh.<name>` removes `<name>`; an opaque whiteout `.wh..wh..opq` removes everything below that directory from lower layers. |

Layer size is the uncompressed size of its entries. The compressed size from
the manifest is shown too, because that's what you pull.

**Waste** is the total size of bytes stored in a layer that don't appear in
the final image, because a later layer modified, deleted or metadata-rewrote
them. Each wasted byte is attributed to both the layer that stored it and the
layer that hid it.

### 6.3 Mapping layers to Dockerfile steps

The image config's `history` array lists every build step. Steps with
`empty_layer: true` (`ENV`, `LABEL`, `CMD`, …) don't produce a layer.
`muscles` pairs the remaining history entries with layers in order, and shows
empty steps as thin rows between layers so the full Dockerfile sequence is
visible. Commands are cleaned up for display (`/bin/sh -c #(nop)` prefixes are
removed; BuildKit's `RUN |2 ARG=...` form is shortened).

### 6.4 Layer summary

Each layer gets a one-line summary, built by rules (not an LLM):

1. Counts and bytes for added, modified, metadata-only and deleted.
2. The 1–3 directories holding most of the changed bytes (rolled up to a
   useful depth, e.g. `/usr/lib/python3.12`, not `/usr`).
3. Notable patterns, each a small detector:
   - package manager caches left behind (`/var/lib/apt/lists`,
     `/var/cache/apk`, `~/.cache/pip`, `~/.npm`)
   - deletions of files that an earlier layer still holds
   - metadata-only rewrites above 1 MB (typically `chown -R` / `chmod -R`)
   - large single files (> 50 MB)

### 6.5 Cache

Layers are content-addressed, so a parsed layer index is valid forever.
Indexes are stored in `$XDG_CACHE_HOME/muscles/layers/<diffID>.idx` (gob or
msgpack, zstd-compressed). A shared base image like `debian:bookworm` is parsed
once and reused by every image built on it. `muscles cache prune` and a size
cap (default 2 GB, oldest first) keep it bounded.

### 6.6 File contents

File contents are not kept in the index. When you open a modified text file to
diff it, `muscles` re-reads just the two layers involved and extracts the two
versions (cached in memory for the session). Files over 1 MB, or binary files,
show size and hash only.

## 7. TUI

### 7.1 Layout

```
┌ muscles  myapp:1.9.0  linux/amd64  612 MB (231 MB compressed)  waste 74 MB ┐
├─ LAYERS ─────────────────────────────┬─ CHANGES · layer 4 ─────────────────┤
│   #  SIZE    +    ~    −  STEP       │ +1,204  ~12  −310   182 MB          │
│   0  74 MB  3.1k  .   .  debian:book │ mostly /usr/lib/python3.12 (151 MB) │
│   1  41 MB   .   220  .  RUN apt-get │ ⚠ /var/lib/apt/lists left: 38 MB    │
│      ·       ENV PYTHONUNBUFFERED=1  │                                     │
│   2  12 KB   3   .   .  WORKDIR /app │ ▾ usr/                              │
│ ▸ 4  182 MB 1.2k 12 310 RUN pip ins… │   ▾ lib/python3.12/site-packages/   │
│   5  96 MB   .  .  .  RUN chown -R…⚠ │     + numpy/            38 MB       │
│   6  8 MB   41   9   .  COPY . .     │     + pandas/           61 MB       │
│                                      │     ~ six.py            34 KB       │
├──────────────────────────────────────┤ ▾ var/lib/apt/lists/                │
│ RUN pip install -r requirements.txt  │     − *_Packages (310)   (in L1)    │
│ created 2026-10-18 14:02  sha256:9f… │                                     │
├──────────────────────────────────────┴─────────────────────────────────────┤
│ tab pane  ↑/↓ move  space fold  c changes/full  w waste  / find  ? help  q │
└────────────────────────────────────────────────────────────────────────────┘
```

- **Header:** image reference, platform, total size (uncompressed and
  compressed), total waste.
- **Layers pane:** one row per layer, empty steps as dim rows between them, ⚠
  on layers with a detected issue. Below it, the full step command and layer
  digest for the selection.
- **Changes pane:** the selected layer's summary, then a collapsible tree.
  Markers and colours: `+` added (green), `~` modified (yellow), `◌` metadata
  only (magenta), `−` deleted (red). Colour is never the only signal.
- **Footer:** context-sensitive key help.

### 7.2 Modes of the right pane

| Mode | Key | Shows |
|------|-----|-------|
| **Changes** (default) | `c` | Only the paths the selected layer touched. |
| **Filesystem** | `c` | The full merged filesystem as of the selected layer, with that layer's changes highlighted. |
| **Waste** | `w` | Every wasted path in the image, biggest first, with the layer that stored it and the layer that hid it. |

### 7.3 File detail

`enter` on a file opens a detail panel: type, size, mode, owner, hash, the
layer that introduced the current version, and its **history across layers**
(e.g. *added in L1 · modified in L4 · chmod in L5*). For modified text files
under 1 MB, `d` shows a unified diff against the previous version.

### 7.4 Key bindings

| Key | Action |
|-----|--------|
| `tab` | Switch pane |
| `j`/`k`, `↑`/`↓` | Move |
| `space`, `h`/`l` | Collapse / expand directory |
| `enter` | File detail |
| `d` | Diff file against previous version |
| `c` | Toggle Changes / Filesystem |
| `w` | Waste view |
| `/` | Find path (fuzzy) |
| `f` | Filter: added, modified, metadata, deleted (toggles) |
| `s` | Sort tree: name / size |
| `y` | Copy path |
| `p` | Choose platform (multi-platform images) |
| `?` | Help |
| `q`, `ctrl+c` | Quit |

### 7.5 Behaviour

- **Progressive loading.** The UI opens as soon as the manifest and config are
  read. Layers fill in as they're indexed, each with a spinner, so you can
  start browsing the base layers while the top ones load.
- Handles trees with 100k+ entries smoothly: only visible rows are rendered.
- Minimum terminal size 80×24; panes stack vertically below 100 columns.
- Works with `NO_COLOR`.

### 7.6 Compare view (`muscles diff A B`)

```
┌ myapp:1.8.0 → myapp:1.9.0      +38 MB     shared: 3 of 7 layers ──────────┐
├─ LAYERS ─────────────────────────────┬─ FINAL FILESYSTEM DIFF ────────────┤
│ = 0  debian:bookworm       74 MB     │ ▾ app/                             │
│ = 1  RUN apt-get install   41 MB     │   ~ main.py          +2 KB         │
│ = 2  WORKDIR /app          12 KB     │   + api/retry.py      4 KB         │
│ ≠ 3  RUN pip install  140→182 MB     │ ▾ usr/lib/python3.12/site-packages │
│ ≠ 4  COPY . .           8→ 8 MB      │   + pandas/          61 MB         │
│                                      │   − simplejson/       1 MB         │
└──────────────────────────────────────┴────────────────────────────────────┘
```

Layers are shared when their digests match, from the bottom up. The right pane
diffs the two final filesystems, with the same markers and file detail as the
single-image view.

## 8. CLI (Cobra)

| Command | Description |
|---------|-------------|
| `muscles <image>` | Open the TUI. |
| `muscles layers <image>` | Table of layers: index, sizes, change counts, waste, step, summary. |
| `muscles changes <image> [--layer N] [--type added,modified,deleted,meta]` | List the changes in one layer (default: all layers). |
| `muscles waste <image> [--top 20]` | Wasted paths, biggest first. |
| `muscles diff <imageA> <imageB> [--tui]` | Compare two images; TUI by default on a TTY. |
| `muscles cache [info\|prune]` | Manage the layer index cache. |
| `muscles completion`, `muscles version` | Standard. |

**Global flags:** `--source`, `--platform`, `-o table|json|plain`,
`--config`, `--no-cache`, `--no-color`, `-v`.

Output defaults to `table` on a TTY and `json` when piped. The JSON carries a
`"schema": "muscles/v1"` field and stays stable across minor releases.

Exit codes: `0` success, `1` error, `2` usage error, `3` image not found or
auth failed.

## 9. Configuration (Viper)

`$XDG_CONFIG_HOME/muscles/config.yaml`, overridable by `MUSCLES_*` env vars
and flags. Everything has a sensible default; no config file is needed.

```yaml
source: auto              # auto | docker | remote | tar | oci
platform: ""              # default: host platform
ui:
  theme: auto             # auto | dark | light
  units: si               # si (MB) | iec (MiB)
  default_pane: changes   # changes | filesystem
  tree_sort: size
cache:
  dir: ""                 # default: $XDG_CACHE_HOME/muscles
  max_size: 2GB
detectors:
  large_file: 50MB
  metadata_rewrite_min: 1MB
ignore:                   # paths hidden from trees and summaries
  - /proc
  - /sys
keys: {}                  # remap any binding, e.g. { waste: W }
```

## 10. Architecture

```
muscles/
├── main.go
├── cmd/                  # Cobra commands; root.go binds Viper
├── internal/
│   ├── config/           # Viper defaults, loading, validation
│   ├── source/           # resolve a reference to a ggcr v1.Image
│   ├── index/            # read layer tars → []Entry; cache by diffID
│   ├── analysis/         # merged FS, change classification, waste, summaries, detectors
│   ├── compare/          # two-image layer alignment and final-FS diff
│   ├── output/           # table / json / plain renderers
│   └── tui/
│       ├── app.go        # root model and routing
│       ├── layers/       # layers pane
│       ├── tree/         # virtualised file tree component
│       ├── detail/       # file detail and diff
│       ├── theme/        # Lip Gloss styles
│       └── keys/         # bindings and help
└── testdata/             # small OCI layouts built for tests
```

Data flows one way: **source → index → analysis → TUI or output**. The
analysis package is pure (no I/O), which keeps it easy to test and lets the
CLI and TUI share it.

For memory, the merged filesystem is a path-keyed tree where each node keeps
the list of `(layer, entry)` versions. A 1 GB image with ~150k files should
stay under ~300 MB RSS.

## 11. Performance targets

| Case | Target |
|------|--------|
| First paint (manifest + config read) | < 300 ms local, < 1 s remote |
| Index a 1 GB local image, cold | < 10 s |
| Re-open any previously indexed image | < 500 ms |
| Tree navigation with 150k entries | No visible lag (< 16 ms per frame) |

## 12. Testing

- **Fixtures:** tiny OCI layouts built in Go test helpers covering added,
  modified, metadata-only and deleted files, opaque whiteouts, hardlinks,
  symlinks, and empty-layer history entries.
- **Analysis:** table-driven tests for classification, waste attribution and
  each detector.
- **Real images:** an opt-in integration test against a few pinned public
  images (e.g. `alpine`, `python:slim`) by digest, with golden JSON output.
- **TUI:** `teatest` golden snapshots at 80×24 and 160×48.
- **CI:** `go vet`, `golangci-lint`, `go test -race`, GoReleaser snapshot build.

## 13. Milestones

| Milestone | Scope |
|-----------|-------|
| **M1: Index** | Sources (daemon, tar, OCI, remote), layer indexing, change classification, `muscles layers` and `muscles changes` with JSON. Useful from the CLI on day one. |
| **M2: TUI** | Layers pane, changes tree, Changes/Filesystem toggle, find, filter, file detail, progressive loading. |
| **M3: Insight** | Waste calculation and view, layer summaries, detectors, text diffs of modified files, layer cache. **v1.0** |
| **M4: Compare** | `muscles diff`, two-image view. **v1.1** |
| **Later** | `muscles check` for CI budgets; package-aware summaries (read dpkg/apk/rpm databases to say "installed curl 8.10, removed libssl1.1"); export a report as Markdown. |

## 14. Open questions

1. **Daemon export speed.** `pkg/v1/daemon` exports the whole image through
   the Docker API, which is slow for large images. Is it worth reading
   containerd's content store directly when available?
2. **Hash choice.** xxhash is fast but not cryptographic. That's fine for
   change detection; is a SHA-256 option worth offering for audit use?
3. **Name.** Keep `muscles`, or rename to something layer-related now that the
   project has changed?
