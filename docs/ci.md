# CI

muscles runs the same in CI as on your laptop. In a pipeline you usually want two things:

1. **A report** reviewers can read: what this change does to the image.
2. **A gate**: fail the build when the image breaks a budget.

No Docker daemon is needed. Build the image to a tarball or an OCI layout and point muscles at the file, and read the baseline (usually the main branch's image) straight from the registry.

## GitHub Actions

The muscles action does both in one step:

```yaml
name: image
on: pull_request

jobs:
  image:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: read        # to read the baseline from ghcr.io
      pull-requests: write  # to comment
    steps:
      - uses: actions/checkout@v4

      - uses: docker/setup-buildx-action@v3

      - name: Build the image to a tarball
        uses: docker/build-push-action@v6
        with:
          context: .
          outputs: type=docker,dest=app.tar

      - name: Log in to read the baseline
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - uses: rossedman/muscles@v0.0.4
        with:
          image: app.tar
          baseline: ghcr.io/acme/app:main
          dockerfile: Dockerfile
```

It:

1. installs the muscles release matching the action's tag (`@v0.0.4` installs v0.0.4) and checks it against the release's checksums,
2. caches layer indexes between runs,
3. writes the report to the job summary: the checks, what changed since `baseline`, and what to fix first,
4. runs `muscles images check` with the budgets in `.muscles.yaml`, so failures and warnings show up as annotations on the Dockerfile lines at fault, right in the pull request's diff,
5. posts the report as a comment on the pull request and updates that same comment on every push, and
6. fails the step when a budget fails.

| Input | Default | Meaning |
|---|---|---|
| `image` | (required) | The image: a tarball, an OCI layout or a registry reference. |
| `baseline` | | The image to compare with and measure growth against. |
| `dockerfile` | | The Dockerfile, relative to the repository, so fixes and annotations name its lines. |
| `check` | `true` | Run the budgets and fail the step when one fails. |
| `comment` | `true` | Comment on pull requests. Needs `pull-requests: write`; without it (as on pull requests from forks) the action warns and carries on. |
| `args` | | More flags for `images check`, like `--max-size 800MB`. |
| `version` | the action's tag | The muscles release to install, or `latest`. |
| `install` | `true` | `false` uses the `muscles` already on `PATH`. |
| `cache` | `true` | Cache layer indexes with `actions/cache`. |
| `token` | `github.token` | Token for the release download and the comment. |

Outputs: `ok` (`true` or `false`), `size`, `waste` and `size-delta` in bytes, and `report`, the path of the Markdown report.

The action runs on Linux and macOS runners. With several images in one workflow, each gets its own comment.

### Without the action

The same steps by hand:

```yaml
      - name: Install muscles
        run: go install github.com/rossedman/muscles/cmd/muscles@latest

      - name: Report what changed
        run: muscles images diff ghcr.io/acme/app:main app.tar -o markdown >> "$GITHUB_STEP_SUMMARY"

      - name: Check budgets
        run: muscles images check app.tar --baseline ghcr.io/acme/app:main --dockerfile Dockerfile
```

`images check` reads the budgets from `.muscles.yaml` in your repository (see [Checks and budgets](checks.md)) and exits 5 when one fails. When `GITHUB_ACTIONS` is set, it also prints `::error` and `::warning` workflow commands, so failures and warnings show up as annotations on the run. Each annotation says how to fix the problem. Set `dockerfile: Dockerfile` in `.muscles.yaml` (or pass `--dockerfile`) and annotations about a layer are placed on its Dockerfile line, with the path made relative to `$GITHUB_WORKSPACE`, so they show up in the pull request's diff. Annotations go with table and plain output only, never into JSON or Markdown.

!!! tip "No setup-go?"
    Use the container image instead of `go install`:

    ```yaml
    - name: Check budgets
      run: docker run --rm -v "$PWD:/work" -w /work ghcr.io/rossedman/muscles images check app.tar
    ```

    Or download a release archive with `curl`. Archives are listed on the [releases page](https://github.com/rossedman/muscles/releases).

## Other CI systems

Nothing above is GitHub-specific except the annotations and `$GITHUB_STEP_SUMMARY`. Elsewhere:

- Run `muscles images check` and fail the job on a non-zero exit.
- Use `-o markdown` for anything that renders Markdown (GitLab merge request notes, Buildkite annotations), or `--json` for your own tooling.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success; for `images check`, every rule passed (warnings never fail). |
| 1 | A runtime error (the registry failed, a layer couldn't be read). |
| 2 | Usage: a bad flag, argument or config file. |
| 3 | Registry authentication failed. Log in. |
| 4 | The image doesn't exist. |
| 5 | `images check` found a failing rule. |
| 130 | Interrupted. |

Exit code 5 is used only for a failed check, so a pipeline can tell "the image is too big" from "the pipeline is broken".

## Speed

Indexing reads every byte of every layer once. Layers are identified by content, so muscles caches each layer's index forever, keyed by its digest: the base image's layers are read once and reused by every build. The action caches it for you; by hand, cache `~/.cache/muscles` (or set `--cache-dir`) with `actions/cache`. The cache is capped at `cache.max_size` (2 GB).
