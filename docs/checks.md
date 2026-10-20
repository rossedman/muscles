# Checks and budgets

`muscles images check IMAGE` checks an image against budgets and forbidden paths and exits 5 if any fails.

![muscles images check](assets/terminal/check.png)

## Configure

Put the budgets in `.muscles.yaml` next to your Dockerfile and commit it. muscles finds the nearest one by searching up from the working directory.

```yaml
# .muscles.yaml
check:
  max_size: 800MB          # uncompressed
  max_compressed: 300MB    # what clients pull
  max_waste: 50MB          # or a share of the image, like "10%"
  max_growth: 5%           # vs --baseline; or bytes, like "50MB"
  max_layers: 40

  forbid:                  # must not exist in the final image
    - "**/.env"
    - "**/.git/"           # a trailing / matches directories only
    - "**/*.pem"
    - "**/id_rsa"
  allow:                   # exceptions to forbid
    - "/etc/ssl/certs/*.pem"
```

Every budget can also come from a flag (`--max-size 800MB`) or an environment variable (`MUSCLES_CHECK_MAX_SIZE=800MB`). Flags win over the environment, which wins over the file. See [Configuration](configuration.md).

Rules that aren't configured are skipped. With nothing configured, `images check` only reports warnings and exits 0.

## The rules

| Rule | Fails when | Detail on failure |
|---|---|---|
| `size` | The uncompressed image is bigger than `max_size`. | The biggest layer. |
| `compressed` | The compressed image is bigger than `max_compressed`. | |
| `waste` | Waste is bigger than `max_waste` (bytes, or a percentage of the image size). | The biggest waste, and why. |
| `growth` | The image grew more than `max_growth` compared with `--baseline` (bytes, or a percentage of the baseline). Skipped without `--baseline`. | |
| `layers` | The image has more layers than `max_layers`. | |
| `forbid` | A path in the final image matches a `forbid` pattern and no `allow` pattern. Up to five paths are listed per pattern. | The layer that added it. |

Sizes accept SI units (`KB`, `MB`, `GB`: powers of 1000) and IEC units (`KiB`, `MiB`, `GiB`: powers of 1024).

## Patterns

`forbid` and `allow` are glob patterns matched against every path in the final filesystem:

- `*` matches within one path segment, `**` across any number of segments: `**/.env` matches `/.env` and `/app/config/.env`.
- A leading `/` is optional: `/app/*.key` and `app/*.key` are the same.
- A trailing `/` matches directories only: `**/.git/` matches the directory, not a file named `.git`.
- `{a,b}` and `[abc]` work as in shells.

Deleted paths are not in the final filesystem, so they don't match. A secret that was added in one layer and deleted in a later one is still in the image; `muscles changes list IMAGE --type deleted` shows those.

## Warnings

The layer notes ([Reading layers](layers.md#notes)) are reported as warnings: package caches left behind, deletions that free nothing, permission-only rewrites and large files. They never fail the check. Turn them down in `warn:` settings.

## Output

```sh
muscles images check app.tar --json | jq '.results[] | select(.status == "fail")'
```

The JSON report has `ok`, `failed`, `warnings` and one result per rule with `rule`, `status` (`pass`, `fail`, `warn`, `skip`), `message` and `detail`.
