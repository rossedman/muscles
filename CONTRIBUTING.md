# Contributing

Thanks for helping with muscles. Issues and pull requests are welcome:
bugs, docs, new notes (detectors), and features.

For a security problem, don't open an issue: see [SECURITY.md](SECURITY.md).

## Before you start

- **Bugs**: open an [issue](https://github.com/rossedman/muscles/issues/new/choose)
  with `muscles version`, the command you ran, and its output with `-v`. If
  you can, say which image (or attach `--json` output).
- **Features**: open an issue first for anything bigger than a small fix,
  so we can agree on the shape before you write it. The
  [design notes](docs/design.md) (D1 onward) explain why muscles works the
  way it does; a change that goes against one should say why.
- **Notes** are a good place to start: a new package cache path or a new
  detector lives in `internal/analysis/notes.go`, with a test.

## Setting up

You need Go (the version in `go.mod`); Docker is optional (tests build
images in memory).

```bash
git clone https://github.com/rossedman/muscles && cd muscles
make build
./muscles --help
```

`docs/design.md` explains the layout and decisions, and `CLAUDE.md` is a
short map of the repository.

## Checks

Run these before you push; CI runs the same and fails a pull request on any
of them:

```bash
make check     # gofmt -l (must print nothing), go vet, one command per file,
               # a doc comment on every function, go test
make cover     # total coverage must stay at 90% or more
make lint      # golangci-lint (see .golangci.yml)
make vuln      # govulncheck
make bench-smoke  # every benchmark once
make fuzz      # each fuzz target for 10s (CI: 30s; FUZZTIME=1m to change)
make docs      # mkdocs build --strict, when docs/, overrides/ or mkdocs.yml change
               # (pip install -r docs/requirements.txt first)
```

## Writing a change

- **Commands**: one command per file in `internal/cli/`, each with `Short`,
  `Long` and three `Example` lines (the common path, a modifier, a piped or
  scripted one). Follow [CLI Style](https://cli.style): data on stdout,
  everything else on stderr, `--json` for anything that returns data, errors
  that say what to run next.
- **Tests**: cover the failure paths, not only the happy one. Command tests
  go through the harness in `internal/cli/cli_test.go`; test images come
  from `internal/testimg`.
- **Output**: plain and JSON output are interfaces. Adding a JSON field is
  fine; removing, renaming or retyping one isn't (the schema is
  `muscles/v1`). Say so in the pull request if plain output changes.
- **Docs**: a behavior change isn't done until its docs are: the page in
  `docs/`, `docs/cli.md`, and the README when commands change.
- **Commits**: an imperative subject ("Add a yarn cache note"), and a body
  that says why.

[`REVIEW.md`](REVIEW.md) is the checklist reviews use.

## Critical paths

These functions read untrusted input or run once per file in an image, so
a change to one needs a benchmark (compare with `make bench` and
[benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) before and
after), and the ones that read input need a fuzz target asserting real
properties, not just "doesn't panic". A fuzzer's failing input goes in the
package's `testdata/fuzz/` with the fix.

| Function | Why | Benchmark | Fuzz |
|---|---|---|---|
| `index.ReadTar` | parses every layer tar, from anyone | ✓ | ✓ |
| `pkgdb.Parse` (dpkg, apk, rpm) | parses package databases from images | ✓ | ✓ |
| `dockerfile.Parse`, `dockerfile.Lines` | parses the user's Dockerfile | ✓ | ✓ |
| `size.ParseBytes`, `size.ParseLimit` | parses budgets from flags, env and config | ✓ | ✓ |
| `source.ParsePlatform` | parses `--platform` | ✓ | ✓ |
| `analysis.Analyze` | replays every entry of every layer | ✓ | |
| `analysis.RollupChanges`, `analysis.RollupWaste` | run over every change and waste item | ✓ | |
| `compare.Images`, `compare.Filesystems` | compare two whole filesystems | ✓ | |

## Regenerating the docs graphics

The banner, social preview and diagram are HTML in `docs/assets/src/`,
rendered with `python3 docs/assets/render.py`. Terminal screenshots come
from `docs/assets/termshot.py` running the real binary against the demo
images (`MUSCLES_DEMO_DIR=/tmp/demo go test ./internal/testimg -run TestWriteDemo`).
Both need `pip install playwright pyte` and a Chromium (`CHROME=/path`).

## License

By contributing, you agree your contribution is licensed under the
[MIT License](LICENSE), like the rest of muscles.
