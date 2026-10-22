# Output formats

Every command renders the same values four ways. Choose with `-o` / `--output`, or `--json`.

| Format | When | Shape |
|---|---|---|
| `table` | Default on a terminal | Aligned columns with a header and totals. Color is decoration only: markers and words carry the meaning. |
| `plain` | Default when stdout isn't a terminal | One tab-separated record per line, no header, no color, exact byte counts. For `cut`, `sort`, `awk` and `grep`. |
| `json` | `--json` or `-o json` | One JSON object. |
| `markdown` | `-o markdown` | GitHub-flavored Markdown tables, for pull requests and job summaries. |
| `sarif` | `-o sarif`, `images check` only | A SARIF 2.1.0 log of the failures and warnings, for code scanning. |

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
     "dockerfile_line": 3,
     "packages": {"installed": [{"manager": "dpkg", "name": "curl", "version": "7.88.1-10"}], "removed": [], "upgraded": []},
     "notes": [{"kind": "package_cache", "message": "apt lists left behind: 12 MB", "bytes": 12000000, "files": 41,
                "path": "/var/lib/apt/lists", "fix": "End this RUN with `&& rm -rf /var/lib/apt/lists/*`."}]}
  ],
  "empty_steps_after": ["CMD [\"python\" \"main.py\"]"]
}
```

Note kinds: `package_cache`, `hidden_delete`, `permission_rewrite`, `overwrite`, `large_file`. Every note has a `fix`; `files` counts the files involved and `stored_in` lists the layers that still store the bytes, when that applies.

`dockerfile_line` is present only with `--dockerfile` and a matching line. `packages` is present only on layers that changed the dpkg, apk, rpm or pip package databases; the packages are `{manager, name, version}`, upgrades `{manager, name, from, to}`.

`images inspect`:

```json
{
  "schema": "muscles/v1",
  "image": {"ref": "app:dev", "...": "as in layers list"},
  "saves": 108000000,
  "fixes": [
    {"kind": "permission_rewrite", "layer": 5, "step": "RUN chown -R app /app", "dockerfile_line": 14, "saves": 60000000,
     "problem": "Stores 1,204 files, 60 MB again only to change owner or permissions; the first copy is still in L4.",
     "fix": "Set the owner as the files are added (`COPY --chown=USER:GROUP`, ...) and drop this chown."}
  ],
  "worth_a_look": [{"kind": "large_file", "layer": 4, "step": "COPY . .", "saves": 0, "problem": "A single 80 MB file: /app/models/base.onnx.", "fix": "..."}],
  "biggest_layers": [{"index": 3, "step": "RUN pip install -r requirements.txt", "size": 140000000, "summary": "mostly /usr/local/lib/python3.12/site-packages (139 MB)"}]
}
```

Fixes are sorted by `saves`, biggest first, and `saves` at the top is their sum. `kind` is a note kind.

`changes list`: `{schema, image, layer, changes: [{layer, path, dir, type, kind, size, prev_size, prev_layer, count}]}`. `layer` is `null` without `--layer`; `prev_layer` is `-1` when there was no earlier version; `dir: true` marks a rolled-up directory of `count` changes.

`waste list`: `{schema, image, waste, items: [{path, dir, size, count, stored_in, hidden_by, reason}]}`.

`images diff`: `{schema, base, target, size_delta, compressed_delta, shared_layers, layers: [{index, shared, step, base_size, target_size}], changes: [...], summary, packages: {installed, removed, upgraded}}`. A layer missing from one image has `null` for its size. `summary` is the one-sentence verdict the table starts with.

`images check`: `{schema, ok, image, baseline, failed, warnings, results: [{rule, status, message, detail, fix, layer, dockerfile_line}]}`. `fix` is set on failures and warnings; `layer` and `dockerfile_line` when a layer is at fault (and its line is known).

`config list`: `{schema, user_file, project_file, settings: [{key, value, origin, env, help}]}`.

`cache list`: `{schema, dir, bytes, max_bytes, layers: [{diff_id, bytes, used}]}`.

`help --json`: the command tree, `{name, path, short, usage, examples, flags, global_flags, commands}`.

`version --json`: `{version, commit, go, platform}`.

Errors under `--json` are an object on stderr: `{"error": {"code", "message", "run"}}`. See [CLI reference](cli.md#streams-and-errors).

## Markdown

Markdown output has a heading and a table; `images inspect` is a numbered list of fixes, `images check` adds a **How to fix** list, and `images diff` starts with its one-sentence summary, adds a layers table and a packages table, and folds long file lists into `<details>`. It's made for `$GITHUB_STEP_SUMMARY` and pull request comments:

```sh
muscles images diff ghcr.io/acme/app:main app.tar -o markdown >> "$GITHUB_STEP_SUMMARY"
```

## SARIF

`muscles images check IMAGE -o sarif` writes a [SARIF 2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html) log, the format GitHub code scanning, GitLab, Azure DevOps and editor viewers read. The exit code is the same as with any other format: 5 when a rule fails.

```sh
muscles images check app.tar --dockerfile Dockerfile -o sarif > muscles.sarif
```

- **Results** are the failures (level `error`) and warnings (level `warning`). Rules that passed or were skipped are left out: SARIF lists findings.
- **Rules** have ids like `muscles/size`, `muscles/forbid` and `muscles/package-cache`, with a description, the fix, and a link to these docs.
- **Locations** are files in the repository, relative to `$GITHUB_WORKSPACE` (or the working directory), because code scanning shows findings in the repository's files:
    1. the Dockerfile line that made the layer at fault (with `--dockerfile`),
    2. else the line of `.muscles.yaml` that set the budget, when it was set there,
    3. else the first line of the Dockerfile, or of `.muscles.yaml`.

    With no Dockerfile and no project file in the repository, results have no location, and GitHub code scanning won't accept them: set `dockerfile` in `.muscles.yaml`.
- **Fingerprints** (`partialFingerprints["muscles/v1"]`) come from the rule and what it is about (a forbidden path, or a layer's step and the note's path), never sizes or the image's name, so a finding stays one alert from build to build, and a fixed one closes.

Each result has `properties.image` and, when a layer is at fault, `properties.layer`.
