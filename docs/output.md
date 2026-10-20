# Output formats

Every command renders the same values four ways. Choose with `-o` / `--output`, or `--json`.

| Format | When | Shape |
|---|---|---|
| `table` | Default on a terminal | Aligned columns with a header and totals. Color is decoration only: markers and words carry the meaning. |
| `plain` | Default when stdout isn't a terminal | One tab-separated record per line, no header, no color, exact byte counts. For `cut`, `sort`, `awk` and `grep`. |
| `json` | `--json` or `-o json` | One JSON object. |
| `markdown` | `-o markdown` | GitHub-flavored Markdown tables, for pull requests and job summaries. |

## Plain

Byte sizes are exact integers and change types are words (`added`, `modified`, `meta`, `deleted`), so lines sort and filter cleanly:

```sh
# The ten biggest files a layer added
muscles changes list app:dev --layer 3 --type added --files -o plain | sort -t$'\t' -k3 -rn | head
```

An empty result prints nothing, so `wc -l` counts zero.

## JSON

Every object has `"schema": "muscles/v1"`. Within `v1`, fields are only ever added, never removed, renamed or retyped. Sizes are integer bytes. Lists are `[]` when empty, never `null`.

`layers list`:

```json
{
  "schema": "muscles/v1",
  "image": {"ref": "app:dev", "source": "docker", "platform": "linux/amd64", "digest": "sha256:…",
            "size": 401000000, "compressed": 138000000, "waste": 108000000, "layers": 6},
  "layers": [
    {"index": 1, "diff_id": "sha256:…", "digest": "sha256:…", "size": 41000000, "compressed": 15000000,
     "step": "RUN apt-get install -y curl git", "created": "2026-10-18T14:02:00Z",
     "empty_steps_before": ["ENV PYTHONUNBUFFERED=1"],
     "counts": {"added": 220, "modified": 3, "meta": 0, "deleted": 0},
     "wasted": 12000000, "summary": "mostly /usr (28 MB)",
     "mostly": [{"path": "/usr", "bytes": 28000000}],
     "notes": [{"kind": "package_cache", "message": "apt lists left behind: 12 MB", "bytes": 12000000, "path": "/var/lib/apt/lists"}]}
  ],
  "empty_steps_after": ["CMD [\"python\" \"main.py\"]"]
}
```

Note kinds: `package_cache`, `hidden_delete`, `permission_rewrite`, `large_file`.

`changes list`: `{schema, image, layer, changes: [{layer, path, dir, type, kind, size, prev_size, prev_layer, count}]}`. `layer` is `null` without `--layer`; `prev_layer` is `-1` when there was no earlier version; `dir: true` marks a rolled-up directory of `count` changes.

`waste list`: `{schema, image, waste, items: [{path, dir, size, count, stored_in, hidden_by, reason}]}`.

`images diff`: `{schema, base, target, size_delta, compressed_delta, shared_layers, layers: [{index, shared, step, base_size, target_size}], changes: [...]}`. A layer missing from one image has `null` for its size.

`images check`: `{schema, ok, image, baseline, failed, warnings, results: [{rule, status, message, detail}]}`.

`config list`: `{schema, user_file, project_file, settings: [{key, value, origin, env, help}]}`.

`cache list`: `{schema, dir, bytes, max_bytes, layers: [{diff_id, bytes, used}]}`.

`help --json`: the command tree, `{name, path, short, usage, examples, flags, global_flags, commands}`.

`version --json`: `{version, commit, go, platform}`.

Errors under `--json` are an object on stderr: `{"error": {"code", "message", "run"}}`. See [CLI reference](cli.md#streams-and-errors).

## Markdown

Markdown output has a heading and a table; `images diff` adds a layers table and folds long file lists into `<details>`. It's made for `$GITHUB_STEP_SUMMARY` and pull request comments:

```sh
muscles images diff ghcr.io/acme/app:main app.tar -o markdown >> "$GITHUB_STEP_SUMMARY"
```
