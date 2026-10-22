# CLI reference

muscles reads like a sentence: `muscles <noun> <verb> <target> --modifier`. Every command has `--help` with real examples; `-h` prints a short screen. Bare `muscles` prints help.

```text
Start here
  muscles IMAGE                    what to fix first, and what each fix saves

Explain an image
  muscles layers list IMAGE        each layer: size, changes, step, notes
  muscles changes list IMAGE       the paths layers added, modified, rewrote or deleted
  muscles waste list IMAGE         hidden bytes, biggest first, and the layers responsible

Compare and gate
  muscles images inspect IMAGE     the same as `muscles IMAGE`
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

## Shortcuts

Shortcuts save typing; the full commands are what you put in scripts.

| You type | muscles runs |
|---|---|
| `muscles app:dev` | `muscles images inspect app:dev` |
| `muscles layers app:dev` | `muscles layers list app:dev` |
| `muscles changes app:dev --layer 3` | `muscles changes list app:dev --layer 3` |
| `muscles waste app:dev` | `muscles waste list app:dev` |

A word that is a command, or close to one (`lyers`), is never taken as an image, so typos still get a suggestion.

## Next steps

At a terminal, a command that has an obvious follow-up ends with one line on stderr saying what to run next, with the real image filled in:

```text
Next: muscles changes list app:dev --layer 5   (the files behind fix 1)
```

It is printed only when stderr is a terminal, never in pipes, CI logs or with `--quiet`, and never on stdout.

## Global flags

| Flag | Meaning |
|---|---|
| `-o, --output FORMAT` | `table`, `plain`, `json` or `markdown`, and `sarif` for `images check`. Default: `table` on a terminal, `plain` otherwise. |
| `--json` | Shorthand for `--output json`. Wins over `--output`. |
| `--source KIND` | `auto`, `docker`, `remote`, `tar` or `oci`. Config key `source`. |
| `--platform OS/ARCH[/VARIANT]` | Platform of a multi-platform image. Default: Linux on this machine's architecture. Config key `platform`. |
| `--config FILE` | Project config file. Default: the nearest `.muscles.yaml`. |
| `--cache-dir DIR` | Layer index cache. Default: `$XDG_CACHE_HOME/muscles`. Config key `cache.dir`. |
| `--dockerfile PATH` | The Dockerfile the image was built from. Layers, fixes and check results then name the line that made them (`Dockerfile:12`), and GitHub annotations point at it. Config key `dockerfile`. |
| `--fresh` | Index every layer again instead of using the cache. |
| `--timeout DURATION` | Give up after this long, like `5m`. Default: no limit. |
| `--no-color` | No color. Also `NO_COLOR=1`, `TERM=dumb`, and whenever the stream isn't a terminal. |
| `-q, --quiet` | Print only errors on stderr. |
| `-v, --verbose` | Print detail on stderr, like which layers came from the cache. Wins over `--quiet`. |
| `-h, --help` | `-h` is a short screen; `--help` is everything. |

## images inspect

```text
muscles images inspect IMAGE [--limit N]
muscles IMAGE [--limit N]
```

One screen: the image's size, what it costs to pull, how much is waste, then **Fix these first**: each fix with what it saves, the layer and Dockerfile step (and line, with `--dockerfile`), the problem and the change to make. Then files worth a look and the three biggest layers.

Every wasted byte is accounted for, whatever the `warn.*` thresholds, and each is counted by one fix only, so the savings add up to the image's waste.

| Flag | Meaning |
|---|---|
| `--limit N` | Show at most N fixes in table and Markdown output (default 5); the rest are summed in one line. `0` shows all. Plain and JSON output always list every fix. |

Plain output: `rank, saves, layer, dockerfile line, step, problem, fix`, one line per fix (line `0` when unknown). See [Finding waste](waste.md#fix-it).

## layers list

```text
muscles layers list IMAGE
```

One row per layer: index, uncompressed size, counts of added (`+`), modified (`~`), rewritten (`◌`) and deleted (`-`) files, the Dockerfile step, and a note: what the layer did (packages it installed, or where its bytes went) or a warning. Steps without a layer are dimmed between rows. See [Reading layers](layers.md).

Plain output: `index, size, compressed, added, modified, meta, deleted, step, note, dockerfile line, fix`, one line per layer.

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

The output starts with one sentence: how much bigger or smaller TARGET is, the step it came from, the biggest file changes and the packages that changed. Then the layers, the packages installed, upgraded and removed, and the filesystem changes.

Plain output is the filesystem changes: `type, size, path, count, note`. See [Comparing images](diff.md).

## images check

```text
muscles images check IMAGE [--baseline IMAGE] [--max-size SIZE] [--max-compressed SIZE]
                           [--max-waste SIZE|PCT] [--max-growth SIZE|PCT] [--max-layers N]
```

Each `--max-*` flag overrides the matching `check.*` setting. Exits 5 if a rule fails. Every failure and warning says how to fix it, in a **How to fix** section after the table.

Plain output: `status, rule, result, detail, fix, dockerfile line`, one line per rule. `-o sarif` writes the failures and warnings as a SARIF 2.1.0 log for code scanning ([SARIF](output.md#sarif)); other commands refuse it. See [Checks and budgets](checks.md).

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
