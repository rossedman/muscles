# Configuration

muscles works with no configuration. Settings exist for budgets in CI, note thresholds and a few preferences.

## Precedence

Each setting is resolved in this order; the first that sets it wins:

1. **Flag**: `--max-size 800MB`
2. **Environment**: `MUSCLES_CHECK_MAX_SIZE=800MB` (the key in upper case, `.` → `_`)
3. **Project file**: the nearest `.muscles.yaml` (or `.muscles.yml`), searching up from the working directory. `--config FILE` names another one instead.
4. **User file**: `$XDG_CONFIG_HOME/muscles/config.yaml` (on macOS without `XDG_CONFIG_HOME`: `~/Library/Application Support/muscles/config.yaml`)
5. **Default**

`muscles config list` shows every setting, its value and which layer set it:

```text
$ muscles config list
KEY                      VALUE   ORIGIN
source                   auto    default
check.max_size           800MB   project /src/app/.muscles.yaml
check.max_layers         40      env MUSCLES_CHECK_MAX_LAYERS
output.units             iec     user /home/me/.config/muscles/config.yaml
...
```

Put what belongs to a repository (budgets, forbidden paths) in the project file and commit it. Put personal preferences (units, cache) in the user file.

An unknown key is an error, so a typo can't silently turn a budget off:

```text
Error: unknown setting "check.max_sise" in .muscles.yaml (did you mean "check.max_size"?)
```

## Settings

| Key | Default | Meaning |
|---|---|---|
| `source` | `auto` | Where images come from: `auto`, `docker`, `remote`, `tar`, `oci`. |
| `platform` | | Platform of multi-platform images, like `linux/arm64`. Default: Linux on this machine's architecture. |
| `output.units` | `si` | `si` (MB, powers of 1000) or `iec` (MiB, powers of 1024). |
| `cache.dir` | `$XDG_CACHE_HOME/muscles` | Layer index cache. |
| `cache.max_size` | `2GB` | Cache cap; least recently used layers go first. |
| `check.max_size` | | Fail when the uncompressed image is bigger. |
| `check.max_compressed` | | Fail when the compressed image is bigger. |
| `check.max_waste` | | Fail when waste is bigger: bytes, or a percentage of the image. |
| `check.max_growth` | | Fail when the image grew more than this compared with `--baseline`: bytes, or a percentage. |
| `check.max_layers` | `0` (off) | Fail when the image has more layers. |
| `check.forbid` | `[]` | Glob patterns that must not exist in the final image. |
| `check.allow` | `[]` | Exceptions to `check.forbid`. |
| `warn.large_file` | `50MB` | Note single files at least this big; `0` turns it off. |
| `warn.permission_rewrite` | `1MB` | Note permission-only rewrites at least this big; `0` turns it off. |
| `warn.package_cache` | `true` | Note package manager caches left in a layer. |

## Example

```yaml
# .muscles.yaml, committed next to the Dockerfile
platform: linux/amd64

check:
  max_size: 800MB
  max_compressed: 300MB
  max_waste: 10%
  max_growth: 50MB
  forbid: ["**/.env", "**/.git/", "**/*.pem"]
  allow: ["/etc/ssl/certs/*.pem"]

warn:
  large_file: 100MB
```

## Files muscles writes

Only the cache, in `cache.dir`. Nothing else, and never in the working directory. To remove muscles completely, delete the binary, `$XDG_CACHE_HOME/muscles` and (if you made one) `$XDG_CONFIG_HOME/muscles`.
