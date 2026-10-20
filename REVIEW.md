# Review checklist

Read with `CLAUDE.md` and `docs/design.md`. Flag a change that:

## Correctness

- Puts two commands in one file, or adds a command without `Short`, `Long`
  and three `Example` lines.
- Writes anything but data to stdout, or data to stderr.
- Prompts without checking `a.interactive()`, which would hang scripts.
- Changes plain or `--json` output that scripts read without saying so, or
  removes, renames or retypes a JSON field (the schema is `muscles/v1`).
- Returns an error that maps to the wrong exit code (`internal/cli/errors.go`),
  or reuses exit code 5 for anything but a failed check.
- Adds I/O to `analysis`, `compare` or `check`.
- Mishandles whiteouts: `.wh.NAME`, opaque `.wh..wh..opq`, and paths deleted
  then re-added in the same layer.
- Adds code without tests for its failure paths (CI requires 90% coverage).

## Security

See `SECURITY.md`. Always flag:

- Anything from an image reaching a command line, a file path outside the
  cache, or a template.
- Credentials in arguments, logs, errors or `--json`.
- A new network call that isn't a read from a registry the user named.
- Unbounded memory or time on a crafted tar (huge headers, deep paths).

## Docs

- Behavior changes without matching updates to `docs/`, `docs/cli.md` or the
  README.
- Docs claiming something the code doesn't do.
- A new docs page missing from `mkdocs.yml`'s nav.

## Not worth flagging

- Style that `gofmt` decides.
- Long `Long` help texts; they're meant to be complete.
