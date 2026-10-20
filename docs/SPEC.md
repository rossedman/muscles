# muscles — Specification

> A CLI that explains container images: what each layer changed, where the
> wasted space is, and what changed between two versions. It works the same on
> your laptop and in CI.

| | |
|---|---|
| **Status** | Draft v0.3 (CLI only; replaces the v0.2 TUI spec) |
| **Author** | Ross Edman |
| **Last updated** | 2026-10-20 |
| **Language** | Go (1.23+) |
| **Libraries** | Cobra, Viper, go-containerregistry, Lip Gloss (table styling only) |

---

## 1. Problem

A container image is a stack of filesystem diffs, but the usual tools barely
show them. `docker history` lists commands and sizes. To see which files a
layer added, changed or deleted, you export the image and diff the tarballs by
hand. In CI it's worse: images grow release after release and nobody notices
until pulls get slow.

Questions that should take seconds to answer:

- Why is this image 1.4 GB? Which layer, which files?
- What did `RUN apt-get install ...` actually put on disk?
- Did my cleanup step actually free space, or does an earlier layer still hold
  those bytes?
- What did this pull request change in the image?
- Did this pull request make the image bigger than we allow?

## 2. Goals

1. **Explain each layer.** Files added, modified, permission-only rewrites and
   deletions, with sizes, next to the Dockerfile step that created the layer,
   plus a one-line summary.
2. **Find waste.** Bytes stored in the image that aren't visible in its final
   filesystem.
3. **Compare two images.** Shared layers, where they split, and a file-level
   diff of the final filesystems.
4. **Gate CI.** Fail a build when the image breaks a size, waste or growth
   budget, or contains a forbidden path.
5. **Same tool, both places.** Readable tables in a terminal; stable JSON and
   Markdown for CI, scripts and pull request comments.
6. **Works without Docker.** Reads from a registry, a tarball or an OCI
   directory as well as the local daemon, so CI runners don't need a Docker
   socket.

## 3. Non-goals

- An interactive TUI. [dive](https://github.com/wagoodman/dive) covers
  interactive browsing; a small `explore` command may come later (§13).
- Building, pushing or modifying images. Generic registry tasks (copy, tag,
  inspect manifests) are left to [crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane)
  and skopeo.
- Vulnerability scanning (Trivy, Grype).
- Windows container images.

## 4. Scenarios

| # | Where | Scenario | Command |
|---|-------|----------|---------|
| S1 | Local | "Why is this image so big?" | `muscles layers app:dev` then `muscles waste app:dev` |
| S2 | Local | "What did layer 4 install?" | `muscles changes app:dev -l 4` |
| S3 | Local | "What changed since the last release?" | `muscles diff ghcr.io/acme/app:1.8.0 app:dev` |
| S4 | CI | "Fail the PR if the image breaks our budget." | `muscles check app.tar --baseline ghcr.io/acme/app:main` |
| S5 | CI | "Show reviewers what this PR changed in the image." | `muscles diff ghcr.io/acme/app:main app.tar -o markdown >> "$GITHUB_STEP_SUMMARY"` |
| S6 | CI | "Never ship secrets or a `.git` directory." | `forbid:` rules in `.muscles.yaml`, enforced by `muscles check` |

## 5. Commands

Every command takes an image reference (§6) and supports
`-o table|json|markdown`.

### 5.1 `muscles layers <image>`

One row per layer, with empty steps (`ENV`, `LABEL`, …) shown dimmed so the
whole Dockerfile sequence is visible.

```
$ muscles layers app:dev
#  SIZE     +      ~    ◌    −    STEP                                NOTE
0  74 MB   3,104   0    0    0    debian:bookworm-slim
1  41 MB     220   3    0    0    RUN apt-get install -y curl git     ⚠ apt lists left behind: 12 MB
   ·                               ENV PYTHONUNBUFFERED=1
2  12 KB       3   0    0    0    WORKDIR /app
3  182 MB  1,204  12    0  310    RUN pip install -r requirements     mostly site-packages (151 MB)
4  96 MB       0   0  412    0    RUN chown -R app /app               ⚠ 96 MB permission-only rewrite
5  8 MB       41   9    0    0    COPY . .

total 401 MB uncompressed · 138 MB compressed · waste 108 MB (27%)
```

Columns: `+` added, `~` modified, `◌` permission/owner-only, `−` deleted.

### 5.2 `muscles changes <image> [-l N] [--type added,modified,meta,deleted] [--path PREFIX] [--min-size SIZE]`

List the paths a layer changed (all layers if `-l` is omitted), biggest first.

```
$ muscles changes app:dev -l 3 --min-size 5MB
TYPE  SIZE    PATH                                                  NOTE
+     61 MB   /usr/local/lib/python3.12/site-packages/pandas/
+     38 MB   /usr/local/lib/python3.12/site-packages/numpy/
~     7 MB    /usr/local/lib/python3.12/site-packages/six.py        was 6 MB (L1)
−     12 MB   /var/lib/apt/lists/                                   bytes still in L1
```

Directories are rolled up by default; `--files` lists individual files.

### 5.3 `muscles waste <image> [--top 20]`

Bytes stored in the image that the final filesystem doesn't show, biggest
first, with the layer that stored them and the layer that hid them.

```
$ muscles waste app:dev
SIZE    PATH                 STORED IN                    HIDDEN BY
96 MB   /app/                L5 COPY . .  (→ also L4)     L4 RUN chown -R app /app (permission-only)
12 MB   /var/lib/apt/lists/  L1 RUN apt-get install       L3 RUN rm -rf /var/lib/apt/lists
total waste 108 MB (27% of uncompressed size)
```

### 5.4 `muscles diff <base> <target> [--files]`

Compare two images.

```
$ muscles diff ghcr.io/acme/app:1.8.0 app:dev
app:1.8.0 → app:dev    401 MB (+38 MB, +10.5%)   compressed 138 MB (+11 MB)

LAYERS (3 of 6 shared)
=  0  debian:bookworm-slim                 74 MB
=  1  RUN apt-get install -y curl git      41 MB
=  2  WORKDIR /app                         12 KB
≠  3  RUN pip install -r requirements      140 MB → 182 MB  (+42 MB)
≠  4  RUN chown -R app /app                 96 MB →  96 MB
≠  5  COPY . .                              12 MB →   8 MB  (−4 MB)

FILESYSTEM (final image)
+  61 MB   /usr/local/lib/python3.12/site-packages/pandas/
−   1 MB   /usr/local/lib/python3.12/site-packages/simplejson/
~  +2 KB   /app/main.py
+   4 KB   /app/api/retry.py
... 37 more changes (use --files for all)
```

Layers count as shared when their digests match, from the bottom up.

### 5.5 `muscles check <image> [--baseline <image>] [--config .muscles.yaml]`

Evaluate budgets and rules, print a report and exit non-zero if any rule fails.

```
$ muscles check app.tar --baseline ghcr.io/acme/app:main
✓ size            401 MB ≤ 800 MB
✓ compressed      138 MB ≤ 300 MB
✗ waste           108 MB > 50 MB         biggest: /app/ (96 MB, permission-only rewrite in L4)
✓ growth          +38 MB ≤ +50 MB vs ghcr.io/acme/app:main
✗ forbid          /app/.env matched "**/.env"      (added in L5 COPY . .)
⚠ large file      /app/models/base.onnx 210 MB (warning only)

2 failed, 1 warning
```

Budgets come from `.muscles.yaml` (§8); any of them can be overridden with
flags (`--max-size`, `--max-compressed`, `--max-waste`, `--max-growth`).
Without a baseline the growth rule is skipped, with a note saying so.

When `GITHUB_ACTIONS=true`, `check` also prints `::error` and `::warning`
workflow annotations so failures show up directly on the run.

### 5.6 Other commands

| Command | Description |
|---------|-------------|
| `muscles cache info` / `muscles cache prune` | Show or clear the layer index cache. |
| `muscles version` | Version, commit, build date. |
| `muscles completion bash\|zsh\|fish` | Shell completion. |

### 5.7 Global flags

| Flag | Description |
|------|-------------|
| `-o, --output table\|json\|markdown` | Default `table` on a TTY, `json` when piped. |
| `--source auto\|docker\|remote\|tar\|oci` | Force an image source (§6). |
| `--platform os/arch[/variant]` | Platform of a multi-platform image. Default: host platform. |
| `--config PATH` | Config file (default: nearest `.muscles.yaml`, then user config). |
| `--cache-dir PATH`, `--no-cache` | Layer cache location, or disable it. |
| `--no-color` | Also respects `NO_COLOR`; colour is off automatically when not a TTY. |
| `-q, --quiet` / `-v, --verbose` | Less or more logging (logs go to stderr, never mixed into stdout). |

### 5.8 Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success (and for `check`: all rules passed; warnings allowed) |
| 1 | `check` found at least one failing rule |
| 2 | Usage or config error |
| 3 | Image not found, or registry auth failed |
| 4 | Any other runtime error |

Exit code 1 is used only for "the check failed", so CI can tell a budget
breach from a broken pipeline.

## 6. Image sources

All sources go through [`go-containerregistry`](https://github.com/google/go-containerregistry),
which exposes one `v1.Image` interface over all of them.

| Source | Reference | Notes |
|--------|-----------|-------|
| Docker / Podman daemon | `app:dev` | Default when the image exists locally. Slower for big images because the daemon exports the whole image. |
| Registry | `ghcr.io/acme/app:main`, or `registry://…` to force it | Streams layers without pulling or writing them to disk. Auth uses `~/.docker/config.json` and credential helpers. |
| Tarball | `app.tar` or `tar://…` | Output of `docker save` or `docker buildx build --output type=docker,dest=app.tar`. |
| OCI layout | `oci://./out[:tag]` | Output of `buildx --output type=oci`. |

With `--source auto` (the default): an existing file path is a tarball, `oci://`
is a layout, a reference found in the daemon is a daemon image, anything else
is a registry reference. If no daemon is running, it goes straight to the
registry. This is the common case in CI.

## 7. How the analysis works

### 7.1 Indexing layers

Each layer tar is read **once**, as a stream. One entry is recorded per tar
header: path, type, size, mode, uid/gid, link target, and an xxhash of the
contents for regular files. Contents themselves are not kept.

### 7.2 Classifying changes

Each entry is compared with the filesystem built from all the layers below it:

| Change | Rule |
|--------|------|
| **Added** `+` | Path doesn't exist below. |
| **Modified** `~` | Path exists below; content hash or type differs. |
| **Permission-only** `◌` | Path exists below with identical content; only mode, owner or mtime differ. The whole file is stored again, so its bytes count as waste. |
| **Deleted** `−` | Whiteout `.wh.<name>`, or opaque whiteout `.wh..wh..opq` (removes everything from lower layers under that directory). |

### 7.3 Waste

Waste is every stored byte that the final filesystem doesn't show, because a
later layer modified, deleted or permission-rewrote that path. Each wasted
byte is attributed to the layer that stored it and the layer that hid it.

### 7.4 Mapping layers to Dockerfile steps

The image config's `history` lists every build step; entries with
`empty_layer: true` produce no layer. Non-empty entries are paired with layers
in order. Commands are tidied for display: the `/bin/sh -c #(nop)` prefix is
removed and BuildKit's `RUN |2 A=1 B=2 …` form is shortened. If the history
can't be paired (some build tools omit it), the STEP column shows `?` and
everything else still works.

### 7.5 Layer summaries and notes

Rule-based (no LLM), so output is deterministic:

- The 1–3 directories holding most of the layer's changed bytes, rolled up to
  a useful depth (`/usr/local/lib/python3.12/site-packages`, not `/usr`).
- Detectors that add a ⚠ note:
  - package manager caches left behind (`/var/lib/apt/lists`, `/var/cache/apt`,
    `/var/cache/apk`, `/root/.cache/pip`, `/root/.npm`, …)
  - deletions that free nothing because an earlier layer still holds the bytes
  - permission-only rewrites over 1 MB (`chown -R`, `chmod -R`)
  - single files over 50 MB

### 7.6 Cache

Layers are content-addressed, so a layer's index never goes stale. Indexes are
stored by diffID in `$XDG_CACHE_HOME/muscles/layers/` (zstd-compressed).
A base image like `debian:bookworm-slim` is indexed once and reused for every
image built on it. The cache is capped at 2 GB by default (oldest dropped
first).

In CI, point `--cache-dir` at a cached directory (e.g. `actions/cache`) so
base layers aren't re-indexed on every run.

## 8. Configuration

Viper loads config in this order, highest priority first:

1. Flags
2. Environment variables, `MUSCLES_` prefix (`MUSCLES_CHECK_MAX_SIZE=800MB`)
3. Project config: the nearest `.muscles.yaml`, searching up from the current
   directory. **This is what CI uses; commit it next to the Dockerfile.**
4. User config: `$XDG_CONFIG_HOME/muscles/config.yaml` (output preferences,
   cache settings)
5. Built-in defaults

No config is required; without one, `check` only runs the default warnings.

```yaml
# .muscles.yaml
image:
  platform: linux/amd64

check:
  max_size: 800MB          # uncompressed
  max_compressed: 300MB    # what users pull
  max_waste: 50MB          # or a percentage, e.g. "10%"
  max_growth: 50MB         # vs --baseline; or a percentage, e.g. "5%"
  max_layers: 40

  forbid:                  # glob patterns that must not appear in the final image
    - "**/.git/"
    - "**/.env"
    - "**/*.pem"
    - "**/id_rsa"
    - "**/node_modules/.cache/"

  allow:                   # exceptions to forbid
    - "/etc/ssl/certs/*.pem"

  warn:                    # these produce warnings, never failures
    large_file: 50MB
    package_cache: true
    permission_rewrite: 1MB

output:
  units: si                # si (MB) or iec (MiB)

cache:
  max_size: 2GB
```

Unknown keys are an error, so typos in a budget don't silently disable it.

## 9. Output formats

| Format | Use |
|--------|-----|
| `table` | Terminals. Aligned columns, colour when the output is a TTY. |
| `json` | Scripts and tools. Includes `"schema": "muscles/v1"`; fields are only ever added within v1, never removed or renamed. Sizes are integer bytes. |
| `markdown` | Pull request comments and `$GITHUB_STEP_SUMMARY`. Tables and a collapsible `<details>` block for long file lists. |

Output is deterministic (stable sort, no timestamps in tables), so results can
be compared between runs.

## 10. CI usage

### 10.1 GitHub Actions

```yaml
name: image
on: pull_request

jobs:
  image-check:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: read
    steps:
      - uses: actions/checkout@v4

      - uses: docker/setup-buildx-action@v3

      - name: Build image to a tarball
        uses: docker/build-push-action@v6
        with:
          context: .
          outputs: type=docker,dest=app.tar

      - name: Install muscles
        run: go install github.com/rossedman/muscles@latest

      - uses: actions/cache@v4
        with:
          path: ~/.cache/muscles
          key: muscles-${{ hashFiles('Dockerfile') }}
          restore-keys: muscles-

      - name: Log in to registry for the baseline
        run: echo "${{ secrets.GITHUB_TOKEN }}" | docker login ghcr.io -u ${{ github.actor }} --password-stdin

      - name: Report image changes
        run: muscles diff ghcr.io/acme/app:main app.tar -o markdown >> "$GITHUB_STEP_SUMMARY"

      - name: Enforce budgets
        run: muscles check app.tar --baseline ghcr.io/acme/app:main
```

### 10.2 Other CI systems

Nothing is GitHub-specific apart from the optional annotations. Elsewhere, run
the same commands and use `-o json` or `-o markdown` with your CI's own report
or comment mechanism.

### 10.3 Distribution for CI

- Static binaries for linux/darwin × amd64/arm64 on GitHub Releases
  (GoReleaser), with checksums.
- A minimal container image (`ghcr.io/rossedman/muscles`) for CI systems that
  run steps in containers.
- `go install` for Go users.

## 11. Architecture

```
muscles/
├── main.go
├── cmd/                  # Cobra commands; root.go binds Viper
│   ├── layers.go
│   ├── changes.go
│   ├── waste.go
│   ├── diff.go
│   ├── check.go
│   └── cache.go
├── internal/
│   ├── config/           # Viper loading, .muscles.yaml discovery, validation, size parsing
│   ├── source/           # reference → ggcr v1.Image (daemon, remote, tar, oci)
│   ├── index/            # stream layer tars → entries; cache by diffID
│   ├── analysis/         # merged filesystem, change classification, waste, summaries, detectors
│   ├── compare/          # layer alignment and final-filesystem diff
│   ├── check/            # budget and forbid rules → results
│   ├── ci/               # CI detection, GitHub annotations
│   └── output/           # table, json, markdown renderers
└── testdata/             # small OCI layouts built by test helpers
```

Data flows one way: **source → index → analysis / compare / check → output**.
`analysis`, `compare` and `check` are pure (no I/O), which makes them easy to
test and keeps every command consistent.

The merged filesystem is a path-keyed tree where each node keeps its list of
`(layer, entry)` versions. A 1 GB image with ~150k files should stay under
~300 MB of memory.

## 12. Quality

### 12.1 Performance targets

| Case | Target |
|------|--------|
| `layers` on a cached image | < 500 ms |
| Index a 1 GB image from a tarball, cold | < 10 s |
| `check` with a cached baseline in CI | < 15 s total for a 1 GB image |

### 12.2 Testing

- **Fixtures:** tiny OCI layouts built in test helpers covering added,
  modified, permission-only and deleted files, opaque whiteouts, hardlinks,
  symlinks and empty-layer history.
- **Analysis and check:** table-driven tests for classification, waste
  attribution, each detector and each rule.
- **Output:** golden files for every command in all three formats; the JSON
  goldens protect the `muscles/v1` schema.
- **Real images:** opt-in integration tests against a few public images pinned
  by digest (e.g. `alpine`, `python:3.12-slim`).
- **CI for this repo:** `go vet`, `golangci-lint`, `go test -race`, GoReleaser
  snapshot build, and `muscles check` run on a fixture image as a smoke test.

## 13. Milestones

| Milestone | Scope |
|-----------|-------|
| **M1: Read and explain** | Image sources, layer indexing, change classification, `layers` and `changes`, table and JSON output. |
| **M2: Waste and summaries** | Waste attribution, `waste`, layer summaries and detectors, layer cache. |
| **M3: Diff and check** | `diff`, `check`, `.muscles.yaml`, Markdown output, GitHub annotations, release binaries and container image, example workflow. **v1.0** |
| **Later** | A published GitHub Action wrapper; posting/updating a PR comment directly; package-aware summaries (read dpkg/apk/rpm databases: "installed curl 8.10"); a small read-only `explore` TUI if browsing long `changes` output becomes painful. |

## 14. Open questions

1. **Baseline in CI.** Is the baseline always the image from the base branch in
   a registry, or should `muscles` also be able to save a baseline file (a
   JSON report) as a build artifact, so no registry access is needed?
2. **Daemon speed.** Exporting through the Docker API is slow for large
   images. Read containerd's content store directly when available?
3. **Name.** Keep `muscles`, or pick something that says "images"?
