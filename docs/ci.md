# CI

muscles runs the same in CI as on your laptop. In a pipeline you usually want two things:

1. **A report** reviewers can read: what this change does to the image.
2. **A gate**: fail the build when the image breaks a budget.

No Docker daemon is needed. Build the image to a tarball or an OCI layout and point muscles at the file, and read the baseline (usually the main branch's image) straight from the registry.

## GitHub Actions

```yaml
name: image
on: pull_request

jobs:
  image:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: read # to read the baseline from ghcr.io
    steps:
      - uses: actions/checkout@v4

      - uses: docker/setup-buildx-action@v3

      - name: Build the image to a tarball
        uses: docker/build-push-action@v6
        with:
          context: .
          outputs: type=docker,dest=app.tar

      - name: Install muscles
        run: go install github.com/rossedman/muscles/cmd/muscles@latest

      - name: Cache layer indexes
        uses: actions/cache@v4
        with:
          path: ~/.cache/muscles
          key: muscles-${{ hashFiles('Dockerfile') }}
          restore-keys: muscles-

      - name: Log in to read the baseline
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Report what changed
        run: muscles images diff ghcr.io/acme/app:main app.tar -o markdown >> "$GITHUB_STEP_SUMMARY"

      - name: Check budgets
        run: muscles images check app.tar --baseline ghcr.io/acme/app:main
```

`images check` reads the budgets from `.muscles.yaml` in your repository (see [Checks and budgets](checks.md)) and exits 5 when one fails. When `GITHUB_ACTIONS` is set, it also prints `::error` and `::warning` workflow commands, so failures and warnings show up as annotations on the run. Each annotation says how to fix the problem. Set `dockerfile: Dockerfile` in `.muscles.yaml` (or pass `--dockerfile`) and annotations about a layer are placed on its Dockerfile line, so they show up in the pull request's diff. Annotations go with table and plain output only, never into JSON or Markdown.

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

Indexing reads every byte of every layer once. Layers are identified by content, so muscles caches each layer's index forever, keyed by its digest: the base image's layers are read once and reused by every build. In CI, cache `~/.cache/muscles` (or set `--cache-dir`) as above. The cache is capped at `cache.max_size` (2 GB).
