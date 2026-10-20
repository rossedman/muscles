# CLI reference

muscles reads like a sentence: `muscles <noun> <verb> <target> --modifier`. Every command has `--help` with real examples; `-h` prints a short screen. Bare `muscles` prints help.

```text
Explain an image
  muscles layers list IMAGE        each layer: size, changes, step, notes
  muscles changes list IMAGE       the paths layers added, modified, rewrote or deleted
  muscles waste list IMAGE         hidden bytes, biggest first, and the layers responsible

Compare and gate
  muscles images diff BASE TARGET  what changed from one image to another
  muscles images check IMAGE       budgets and forbidden paths; exits 5 on failure

Settings and more
  muscles config list              every setting, its value and where it came from
  muscles cache list               cached layer indexes
  muscles cache delete             clear the cache (--preview to look first)
  muscles version                  version, commit and platform
  muscles help [COMMAND]           full help; --json for the command tree
  muscles completion SHELL         bash, zsh, fish or powershell
```

`IMAGE` is a local daemon tag, a registry reference, a tarball or an OCI layout; see [Where images come from](getting-started.md#where-images-come-from). If you leave out a required argument at a terminal, muscles asks for it and then shows the full command; without a terminal it exits 2 and names what's missing.

## Global flags

| Flag | Meaning |
|---|---|
| `-o, --output FORMAT` | `table`, `plain`, `json` or `markdown`. Default: `table` on a terminal, `plain` otherwise. |
| `--json` | Shorthand for `--output json`. Wins over `--output`. |
| `--source KIND` | `auto`, `docker`, `remote`, `tar` or `oci`. Config key `source`. |
| `--platform OS/ARCH[/VARIANT]` | Platform of a multi-platform image. Default: Linux on this machine's architecture. Config key `platform`. |
| `--config FILE` | Project config file. Default: the nearest `.muscles.yaml`. |
| `--cache-dir DIR` | Layer index cache. Default: `$XDG_CACHE_HOME/muscles`. Config key `cache.dir`. |
| `--fresh` | Index every layer again instead of using the cache. |
| `--timeout DURATION` | Give up after this long, like `5m`. Default: no limit. |
| `--no-color` | No color. Also `NO_COLOR=1`, `TERM=dumb`, and whenever the stream isn't a terminal. |
| `-q, --quiet` | Print only errors on stderr. |
| `-v, --verbose` | Print detail on stderr, like which layers came from the cache. Wins over `--quiet`. |
| `-h, --help` | `-h` is a short screen; `--help` is everything. |

## layers list

```text
muscles layers list IMAGE
```

One row per layer: index, uncompressed size, counts of added (`+`), modified (`~`), rewritten (`◌`) and deleted (`-`) files, the Dockerfile step, and a note. Steps without a layer are dimmed between rows. See [Reading layers](layers.md).

Plain output: `index, size, compressed, added, modified, meta, deleted, step, note`, one line per layer.

## changes list

```text
muscles changes list IMAGE [--layer N] [--type TYPES] [--path PREFIX] [--min-size SIZE] [--files] [--limit N]
```

| Flag | Meaning |
|---|---|
| `--layer N` | Only layer N (default: every layer). |
| `--type TYPES` | Comma-separated: `added`, `modified`, `meta`, `deleted`. |
| `--path PREFIX` | Only paths at or below this one. |
| `--min-size SIZE` | Only rows at least this big, like `1MB`. |
| `--files` | List every file; don't roll up directories. |
| `--limit N` | About how many rows to roll up into, per layer (default 30). |

Plain output: `layer, type, size, path, count, note`.

## waste list

```text
muscles waste list IMAGE [--files] [--limit N]
```

Plain output: `size, path, count, reason, stored-in layer, hidden-by layer`. See [Finding waste](waste.md).

## images diff

```text
muscles images diff BASE TARGET [--files] [--limit N]
```

Plain output is the filesystem changes: `type, size, path, count, note`. See [Comparing images](diff.md).

## images check

```text
muscles images check IMAGE [--baseline IMAGE] [--max-size SIZE] [--max-compressed SIZE]
                           [--max-waste SIZE|PCT] [--max-growth SIZE|PCT] [--max-layers N]
```

Each `--max-*` flag overrides the matching `check.*` setting. Exits 5 if a rule fails. See [Checks and budgets](checks.md).

## config list

```text
muscles config list
```

Every setting with its value and origin: `flag --x`, `env MUSCLES_X`, `project PATH`, `user PATH` or `default`. See [Configuration](configuration.md).

## cache list, cache delete

```text
muscles cache list
muscles cache delete [--preview]
```

Deleting the cache loses nothing: the next command reads the layers again.

## help, version

```text
muscles help [COMMAND...] [--json]
muscles version [--json]
```

`help --json` prints the command tree (names, usage, examples, flags) generated from the same definitions as the help text, for scripts and agents. `version --json` prints `version`, `commit`, `go` and `platform`.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success, including help you asked for and empty results. |
| 1 | A runtime error. |
| 2 | Usage: an unknown command or flag, a bad value, a missing argument, an invalid config file. |
| 3 | Registry authentication failed. |
| 4 | The image doesn't exist. |
| 5 | `images check`: a rule failed. |
| 130 | Interrupted (Ctrl-C). |

Codes are part of the interface and never renumbered.

## Streams and errors

stdout carries only data: tables, lines, JSON, Markdown, and help you asked for. Progress, hints, warnings and errors go to stderr. Progress is a single live line, shown only when stderr is a terminal.

Errors are one line saying what failed, then what to run:

```text
Error: registry authentication failed for ghcr.io/acme/app:main: ...
run: docker login ghcr.io
```

With `--json`, an error is an object on stderr:

```json
{"error": {"code": "AUTH_REQUIRED", "message": "...", "run": "docker login ghcr.io"}}
```

Codes: `USAGE`, `CONFIG_INVALID`, `AUTH_REQUIRED`, `NOT_FOUND`, `CHECK_FAILED`, `TIMEOUT`, `INTERRUPTED`, `FAILED`.

Mistyped commands and flags get a suggestion: `unknown command "lyers" for "muscles"` / `Did you mean "layers"?`.
