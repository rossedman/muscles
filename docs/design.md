# Design

How muscles is put together, and the decisions behind it. Read the relevant section before changing a design.

## Layout

```text
cmd/muscles/         entry point
internal/cli/        Cobra commands, one per file; app.go holds shared wiring,
                     usage.go renders help, errors.go maps errors to exit codes
internal/config/     Viper settings: precedence, project and user files, validation
internal/source/     image references → v1.Image (daemon, registry, tarball, OCI)
internal/index/      stream layer tars into entries; the layer cache
internal/analysis/   replay layers: changes, waste, steps, summaries, notes, rollup
internal/compare/    two images: shared layers and the final filesystem diff
internal/check/      budgets and forbidden paths
internal/ui/         terminal detection, theme, banner, tables
internal/size/       parse and format sizes and limits
internal/version/    build version
internal/testimg/    small images built in memory for tests and the docs
docs/, overrides/    the MkDocs site; mkdocs.yml is its nav
```

Data flows one way: **source → index → analysis → compare / check → cli**. `analysis`, `compare` and `check` do no I/O, so they're fast to test and every output format renders the same values.

## Decisions

### D1. A CLI, not a TUI

The work is analysis, and analysis composes: tables for people, lines for pipes, JSON for programs, Markdown for pull requests, and an exit code for CI. A TUI would be a second interface to keep correct, and interactive browsing is already well served by [dive](https://github.com/wagoodman/dive).

### D2. Follow CLI Style

muscles follows the [CLI Style](https://cli.style) guide. In particular:

- **Grammar** (§1, §5): `muscles <noun> <verb>`, few verbs (`list`, `diff`, `check`, `delete`) over few nouns. A bare noun prints its help.
- **Convention** (§8, §14): no flags or config needed for the common path.
- **Precedence** (§11): flags > environment > project > user > defaults, inspectable with `muscles config list`.
- **Outcome naming** (§16, §17): `--preview`, `--fresh`, `--files`.
- **Self-documenting** (§23 to §27): every command has three real examples; `help --json` and `version --json`; help ends with the docs and issues links.
- **Typed streams** (§28): data on stdout, everything else on stderr. Progress is a live line only on a terminal (§33).
- **Machine output** (§30, §46, §51): `--json` on every command, errors as JSON objects, a versioned schema that only grows.
- **Color** (§32, §39): decoration only; each stream is checked separately; `NO_COLOR`, `TERM=dumb` and `--no-color` turn it off.
- **Empty results** (§35): a sentence on a terminal, `[]` in JSON, nothing in plain output.
- **Interaction** (§40): a missing argument is asked for at a terminal, then the full command is shown; without a terminal it's exit 2 naming the argument.
- **Failure** (§42 to §47): suggestions for mistyped commands and flags; errors say what to run; exit codes are documented and stable; a panic asks to be reported.
- **Trust** (§54 to §56): credentials never come from arguments; nothing is sent anywhere but the registries you name; the only file written is the cache.

### D3. go-containerregistry for every source

One library, one `v1.Image` interface, for the daemon, registries, tarballs and OCI layouts. Registry layers are streamed, never pulled. Auth is the Docker keychain (and Podman's auth file), so muscles never handles credentials itself.

### D4. Index once, cache by diffID

A layer's diffID is the digest of its uncompressed content, so its index never changes. Indexes are cached forever (with an LRU size cap), zstd-compressed gob, written atomically. Contents are hashed with xxhash, not stored: muscles needs to know *whether* a file changed, not what it says.

For tarballs and the daemon, go-containerregistry may compute a layer's compressed digest by compressing it, so the cache records those values too. Registries report them for free and are always asked.

### D5. Replay over a tree

Changes are classified by replaying entries over a merged filesystem tree, the way a container runtime stacks layers. That makes the OCI whiteout rules (including opaque directories) direct to implement and lets waste be attributed to exactly two layers: the one that stored the bytes and the one that hid them.

Re-storing a file with identical contents is its own change type (`meta`), because it's the most common avoidable waste (`chown -R`) and it looks like nothing changed.

### D6. Roll up to a budget of rows

A layer can touch tens of thousands of files. Directories whose changes are all alike collapse into one row, and the biggest directories are opened first while the list fits about 30 rows. Mixed directories are always opened. The result reads like a summary for big layers and lists every file for small ones; `--files` turns it off.

### D7. Rules, not a model, for notes

Summaries and notes are deterministic rules (the 80% descent for "mostly", a list of package cache paths, thresholds), so output is the same every run and can be checked in CI.

### D8. Exit code 5 means "the check failed"

CI needs to tell "this image breaks the budget" from "the pipeline broke". Exit 1 stays a runtime error; 5 is only `images check` failing a rule. Warnings never fail.

### D9. Annotations only where they can't corrupt data

GitHub reads workflow commands from stdout. muscles prints them only with table and plain output, never into JSON or Markdown, which are usually piped into a parser or the job summary.

### D10. Fixes claim bytes, so savings add up

`images inspect` ranks fixes by what they save. A byte of waste can look like several problems at once: apt lists left in one layer and deleted in another are both a package cache and a deletion that frees nothing. Each wasted byte is claimed by the first fix that explains it, package caches first because their fix is the most direct, so no byte is counted twice and the savings sum to the image's waste. Inspect ignores the `warn.*` thresholds for the same reason: a total that doesn't add up isn't trustworthy.

### D11. Dockerfile lines by matching, not by build metadata

Images don't record which line made a layer. Rather than require BuildKit provenance, muscles matches the image's steps to the final stage's instructions, from the top layer down, comparing commands with flags (`--mount`, `--chown`) dropped and COPY falling back to its destination. Matching from the top leaves the base image's layers unmatched, and a step that matches nothing simply has no line: a stale Dockerfile degrades, it doesn't mislead.

### D12. Packages from the databases, not from file paths

Package managers already write down what is installed. Reading the dpkg status file, apk's database and pip's `METADATA` as layers are indexed (and caching the result with the index) gives exact names and versions for "what did this layer install" and "what changed between images", at the cost of reading a few small files.

### D13. Shortcuts rewrite argv, with typos left alone

`muscles IMAGE` and `muscles layers IMAGE` are rewritten to the full commands before cobra parses them, so help, flags and errors are those of the full command. A word that is or nearly is a command name is never taken for an image, so `muscles lyers` still says "did you mean layers". Scripts should use the full commands.

### D14. Next hints only for a person

A `Next:` line helps someone exploring at a terminal and is noise in logs, so it is printed on stderr only when stderr is a terminal, and never with `--quiet`.

## Testing

Total coverage must stay at 90% or more (CI enforces it). Images for tests are built in memory by `internal/testimg`; the registry tests use go-containerregistry's in-memory registry; the daemon is faked through `cli.Options`. Command tests drive `cli.Run` with buffers and a fake terminal.
