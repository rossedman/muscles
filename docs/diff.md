# Comparing images

`muscles images diff BASE TARGET` shows what changed from one image to another: a release to the next, the main branch to a pull request, or two ways of building the same thing.

```sh
muscles images diff ghcr.io/acme/app:1.8.0 ghcr.io/acme/app:1.9.0
```

![muscles images diff](assets/terminal/diff.png)

## The header

The first line is the target's uncompressed size and its change from the base, as bytes and a percentage, then the compressed size (what clients pull) and its change.

## Layers

Layers are lined up by position. From the bottom up, layers with the same digest are **shared** (`=`): a client that has the base already has them and won't pull them again. The first layer that differs, and every layer above it, is marked `≠` with its size in both images.

Few shared layers usually means an early step changed: a new base image, or a `COPY` of the whole source tree before installing dependencies. Order Dockerfile steps from least to most often changed to keep more layers shared.

## Filesystem

The second table compares the two final filesystems, the way a container sees them:

| Marker | Meaning |
|---|---|
| `+` | The path is new in the target. |
| `~` | The path's contents, link target or type changed. The note shows the old size. |
| `◌` | Same contents, new permissions or owner. |
| `-` | The path is gone from the target. |

Directories only appear when they were added or removed with nothing inside. Rows roll up by directory like [`changes list`](layers.md#looking-inside-a-layer); `--files` lists every file.

## In a pull request

Write the diff to the GitHub Actions job summary, so reviewers see what a change does to the image:

```sh
muscles images diff ghcr.io/acme/app:main app.tar -o markdown >> "$GITHUB_STEP_SUMMARY"
```

Long file lists are folded into a `<details>` block. See [CI](ci.md) for a complete workflow.
