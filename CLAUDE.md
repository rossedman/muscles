# muscles

A Go CLI that explains container images: what each layer added, modified,
rewrote and deleted, where the wasted space is, what changed between two
images, and whether an image breaks budgets in CI. Docs: `docs/` (MkDocs,
published to GitHub Pages).

## Layout

```
cmd/muscles/         entry point
internal/cli/        Cobra commands, one per file (noun files like layers.go,
                     verb files like layers_list.go); app.go is shared wiring,
                     usage.go renders help, errors.go maps errors to exit codes
internal/config/     Viper settings: flags > MUSCLES_* env > .muscles.yaml > user file > defaults
internal/source/     image references → v1.Image (daemon, registry, tarball, OCI)
internal/index/      layer tars → entries; the layer cache (by diffID)
internal/analysis/   replay layers: changes, waste, steps, notes, rollup (no I/O)
internal/compare/    two images (no I/O)
internal/check/      budgets and forbidden paths (no I/O)
internal/ui/         terminal detection, theme, banner, tables
internal/testimg/    images built in memory for tests and docs screenshots
action.yml, scripts/action/   the GitHub Action (composite, bash)
docs/, overrides/    the site; mkdocs.yml is its nav
tools/               repo checks: doccheck, check-cmds.sh
```

`docs/design.md` explains the decisions (D1 to D9); read the relevant one
before changing a design. The CLI follows https://cli.style.

## Checks

Run before pushing; CI runs the same (`.github/workflows/ci.yml`):

```bash
gofmt -l .                       # must print nothing
go vet ./...
./tools/check-cmds.sh             # one command per file in internal/cli
go run ./tools/doccheck ./...    # a doc comment on every function
golangci-lint run ./...
go test -race ./...              # CI also fails below 90% total coverage:
go test -coverprofile=/tmp/c.out ./... && go tool cover -func=/tmp/c.out | tail -1
mkdocs build --strict -d /tmp/muscles-site   # when docs/, overrides/ or mkdocs.yml change
goreleaser check                 # when .goreleaser.yaml changes
```

- Build the docs outside the repo (`-d`) so `site/` isn't committed.
- Tests need no Docker: `internal/testimg` builds images, the registry is
  go-containerregistry's in-memory one, and the daemon is faked.

## Working here

- A behavior change isn't done until its docs are: the page in `docs/`,
  `docs/cli.md`, `docs/output.md` for JSON, and the README.
- stdout is data only. JSON fields are only ever added (schema `muscles/v1`).
- Commits: an imperative subject and a body saying why.
