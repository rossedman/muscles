# muscles — Specification

> A CLI that explains container images: what each layer changed, where the
> wasted space is, and what changed between two versions. It works the same on
> your laptop and in CI.

| | |
|---|---|
| **Status** | v0.1, implemented. This spec records what was built and why; the [docs](index.md) are the user-facing reference. |
| **Author** | Ross Edman |
| **Last updated** | 2026-10-20 |
| **Language** | Go (1.23+) |
| **Libraries** | Cobra, Viper, go-containerregistry, Lip Gloss (color only), xxhash, zstd, doublestar |
| **Style guide** | [CLI Style](https://cli.style) |

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
  interactive browsing (see design note D1).
- Building, pushing or modifying images. Generic registry tasks (copy, tag,
  inspect manifests) are left to [crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane)
  and skopeo.
- Vulnerability scanning (Trivy, Grype).
- Windows container images.

## 4. Scenarios

| # | Where | Scenario | Command |
|---|-------|----------|---------|
| S1 | Local | "Why is this image so big, and what do I change?" | `muscles app:dev --dockerfile Dockerfile` (`images inspect`), then `muscles waste list app:dev` |
| S2 | Local | "What did layer 4 install?" | `muscles changes list app:dev --layer 4` |
| S3 | Local | "What changed since the last release?" | `muscles images diff ghcr.io/acme/app:1.8.0 app:dev` |
| S4 | CI | "Fail the PR if the image breaks our budget." | `muscles images check app.tar --baseline ghcr.io/acme/app:main` |
| S5 | CI | "Show reviewers what this PR changed in the image." | `muscles images diff ghcr.io/acme/app:main app.tar -o markdown >> "$GITHUB_STEP_SUMMARY"` |
| S6 | CI | "Never ship secrets or a `.git` directory." | `check.forbid` in `.muscles.yaml`, enforced by `images check` |

## 5. Command grammar

Following CLI Style §1 and §5, commands are `muscles <noun> <verb> <target> --modifier`, with few verbs over few nouns. A bare noun prints its help.

| Command | Purpose |
|---|---|
| `images inspect IMAGE` | Size, pull size, waste; ranked fixes with savings, layer, step, Dockerfile line, problem and fix; files worth a look; biggest layers. `--limit`. `muscles IMAGE` runs it. |
| `layers list IMAGE` | One row per layer: size, `+ ~ ◌ -` counts, step, note. Empty steps dimmed between rows. |
| `changes list IMAGE` | Paths changed, rolled up by directory: `--layer`, `--type`, `--path`, `--min-size`, `--files`, `--limit`. |
| `waste list IMAGE` | Hidden bytes with the storing and hiding layers: `--files`, `--limit`. |
| `images diff BASE TARGET` | A one-sentence verdict, shared layers, per-layer size changes, package changes, final filesystem diff. |
| `images check IMAGE` | Budgets and forbidden paths: `--baseline`, `--max-size`, `--max-compressed`, `--max-waste`, `--max-growth`, `--max-layers`. |
| `config list` | Every setting, value and origin. |
| `cache list`, `cache delete [--preview]` | The layer index cache. |
| `help [COMMAND] [--json]`, `version [--json]`, `completion SHELL` | Self-documentation (CLI Style §23 to §26). |

Global flags: `-o/--output table|plain|json|markdown`, `--json` (wins over `--output`), `--source`, `--platform`, `--config`, `--cache-dir`, `--dockerfile`, `--fresh`, `--timeout`, `--no-color`, `-q/--quiet`, `-v/--verbose` (wins over `--quiet`).

Shortcuts: `muscles IMAGE` is `muscles images inspect IMAGE`; `muscles layers|changes|waste IMAGE` runs that noun's `list`. A word that is or nearly is a command is never taken as an image. At a terminal, commands end with a `Next:` hint on stderr.

The [CLI reference](cli.md) documents every flag and output shape.

### 5.1 Change types

| Marker | Name | Rule |
|---|---|---|
| `+` | added | The path doesn't exist below. |
| `~` | modified | The path exists below; contents, link target or type differ. |
| `◌` | meta | The path exists below with identical contents; only permissions, owner or times differ. The whole file is stored again. |
| `-` | deleted | `.wh.NAME`, or `.wh..wh..opq` (removes lower layers' entries under a directory, keeping its own layer's). |

### 5.2 Exit codes

| Code | Meaning |
|---|---|
| 0 | Success, including requested help and empty results |
| 1 | Runtime error |
| 2 | Usage: bad command, flag, argument or config file |
| 3 | Registry authentication failed |
| 4 | Image not found |
| 5 | `images check` found a failing rule |
| 130 | Interrupted |

Exit 5 is only ever a failed check, so CI can tell a budget breach from a broken pipeline. Under `--json`, errors are `{"error": {"code", "message", "run"}}` on stderr.

## 6. Image sources

All sources go through go-containerregistry's `v1.Image`:

| Source | Reference | Notes |
|---|---|---|
| Docker / Podman daemon | `app:dev`, or `docker://` | Asked first in `auto` mode; the daemon exports the whole image. |
| Registry | `ghcr.io/acme/app:main`, or `registry://` | Layers streamed, never pulled. Docker and Podman keychains. |
| Tarball | `app.tar`, or `tar://` | `docker save` or buildx `type=docker`; exactly one image. |
| OCI layout | `./out`, or `oci://DIR[:tag]` | Tag by the `org.opencontainers.image.ref.name` annotation; nested indexes followed. |

Resolution: an explicit prefix, then `--source`, then an existing path (directory: OCI, file: tarball), then anything that can only be a path (starts with `/`, `.` or `~`, or ends in `.tar` without a tag) is a missing tarball, else the daemon and then the registry. Multi-platform images pick `--platform`, or Linux on the host architecture; attestation manifests are skipped.

## 7. Analysis

### 7.1 Indexing

Each layer tar is read once, as a stream. One entry per header: clean absolute path, type, size, permission bits, uid, gid, link target, xxhash64 of the contents. Contents are never kept.

### 7.2 Replay

Layers are replayed over a merged filesystem tree (§5.1). Each wasted byte is attributed to the layer that stored it and the layer that hid it, with a reason (modified, meta, deleted). Re-declared directories are not changes.

### 7.3 Steps

History entries that made a layer are paired with layers aligned at the top (some builders drop base image history); entries without a layer become the empty steps between layers. `CleanStep` turns `/bin/sh -c #(nop) ENV A=1` into `ENV A=1` and BuildKit's `RUN |2 A=1 B=2 /bin/sh -c make # buildkit` into `RUN make`.

### 7.4 Summaries and notes

- **Mostly:** descend from `/` while one directory holds at least 80% of a layer's changed bytes; if none dominates, the top three directories holding at least 10% each.
- **Notes:** `package_cache` (apt, apk, yum, dnf, pip, npm, yarn, Go build cache), `hidden_delete`, `permission_rewrite` (≥ `warn.permission_rewrite`), `overwrite` (≥ `warn.overwrite`), `large_file` (≥ `warn.large_file`). Every note carries a `fix`.
- **Packages:** the dpkg status file, apk's installed database, rpm's database (SQLite, Berkeley DB or ndb) and pip's `.dist-info/METADATA` are parsed as layers are indexed; each layer records the packages it installed, upgraded and removed, and its summary says so.
- **Fixes:** `images inspect` turns notes into fixes, each claiming the wasted bytes it would save (package caches first) so no byte is counted twice and the savings add up to the waste.
- **Dockerfile lines:** with `--dockerfile`, the final stage's instructions are matched to steps from the top down, ignoring flags; unmatched steps have no line.

### 7.5 Rollup

A directory collapses into one row only when every change below it has the same type and previous layer. Starting from the root, mixed directories are always opened, single-entry directories are always opened, and then the biggest directories are opened while the rows fit about `--limit` (30).

### 7.6 Cache

Layer indexes are stored as zstd-compressed gob in `$XDG_CACHE_HOME/muscles/layers/v1/<diffID>.idx`, written atomically, LRU-capped at `cache.max_size` (2 GB). For tarball and daemon sources the cache also records the compressed digest and size, which go-containerregistry otherwise computes by compressing the layer; registries are always asked.

## 8. Configuration

Precedence (CLI Style §11): flags > `MUSCLES_*` environment > project file (nearest `.muscles.yaml`/`.muscles.yml` up from the working directory, or `--config`) > user file (`$XDG_CONFIG_HOME/muscles/config.yaml`) > defaults. Unknown keys are errors with a did-you-mean suggestion.

```yaml
source: auto              # auto | docker | remote | tar | oci
platform: ""              # default: linux on the host architecture
output:
  units: si               # si (MB) | iec (MiB)
cache:
  dir: ""                 # default: $XDG_CACHE_HOME/muscles
  max_size: 2GB
check:
  max_size: 800MB
  max_compressed: 300MB
  max_waste: 50MB         # or "10%" of the image
  max_growth: 5%          # vs --baseline; or bytes
  max_layers: 40
  forbid: ["**/.env", "**/.git/"]
  allow: ["/etc/ssl/certs/*.pem"]
warn:
  large_file: 50MB
  permission_rewrite: 1MB
  package_cache: true
```

## 9. Output

| Format | Default when | Notes |
|---|---|---|
| `table` | stdout is a terminal | Aligned, colored (decoration only), with totals. |
| `plain` | stdout is not a terminal | One tab-separated record per line, exact byte counts, no header (CLI Style §36). |
| `json` | `--json` | `"schema": "muscles/v1"`; fields only ever added; `[]` never `null`. |
| `markdown` | `-o markdown` | For pull requests and job summaries; long diffs folded in `<details>`. |

Progress is one live line on stderr, only when stderr is a terminal. With `GITHUB_ACTIONS=true`, `images check` adds `::error`/`::warning` workflow commands to table and plain output (never JSON or Markdown).

## 10. Distribution

GoReleaser builds static binaries for linux/darwin/windows × amd64/arm64 with completions and a man page, `checksums.txt`, a multi-arch image `ghcr.io/rossedman/muscles` (distroless static), and a Homebrew cask in `rossedman/homebrew-tap`. A pushed `v*` tag runs the release workflow. See [Releasing](releasing.md).

## 11. Architecture

```
cmd/muscles/         entry point
internal/cli/        Cobra commands, one per file
internal/config/     Viper resolution and validation
internal/source/     references → v1.Image
internal/index/      layer tars → entries; the cache
internal/analysis/   replay, waste, steps, notes, rollup (no I/O)
internal/compare/    two images (no I/O)
internal/check/      budgets and forbidden paths (no I/O)
internal/ui/         terminal, theme, banner, tables
internal/size/       sizes and limits
internal/testimg/    in-memory images for tests and docs
```

Data flows one way: source → index → analysis → compare / check → cli. See [Design](design.md) for the decisions (D1 to D9).

## 12. Quality

- **Coverage:** 90% total or more, enforced in CI (currently about 96%).
- **Tests:** in-memory images (`internal/testimg`) cover every change type, opaque whiteouts, links, empty history and mismatched history; the registry tests use go-containerregistry's in-memory registry; the daemon is faked; command tests drive `cli.Run` with buffers and a fake terminal, including prompts, interrupts, timeouts and a crash.
- **CI:** gofmt, vet, `go test -race`, coverage gate, golangci-lint, `goreleaser check`, a strict docs build, and a smoke job that builds muscles' own image and checks it against `.muscles.yaml`.

## 13. Later

- A published GitHub Action wrapper, and posting or updating a pull request comment directly.
- A JSON baseline report, so `images check --baseline report.json` needs no registry access.
- Reading containerd's content store directly, avoiding the daemon's full export.
- A small read-only `explore` TUI, if browsing long `changes list` output proves painful.

## 14. Open questions

1. **Name.** `muscles` is short and easy to type (CLI Style §4) but says nothing about images. Keep it?
2. **Baseline artifacts.** Is reading the baseline from a registry enough, or should a saved JSON report be the default in CI (§13)?
