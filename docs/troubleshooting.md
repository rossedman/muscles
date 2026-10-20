# Troubleshooting

Run any command with `-v` to see where an image came from and which layers were read or cached.

## "registry authentication failed" (exit 3)

The registry wants credentials. muscles uses the same ones as `docker pull`: your Docker config (`~/.docker/config.json`) and its credential helpers. Log in and try again:

```sh
docker login ghcr.io
```

In GitHub Actions, use `docker/login-action` before muscles (see [CI](ci.md)). Podman's login file is read too: `$REGISTRY_AUTH_FILE`, or `containers/auth.json` under `$XDG_RUNTIME_DIR`.

## "image not found" (exit 4)

- A local tag that only exists in Podman or containerd isn't visible to the Docker API. Export it: `podman save app:dev -o app.tar`, then `muscles layers list app.tar`.
- A name without a registry (`app:dev`) that isn't in the local daemon is looked up on Docker Hub. Use the full reference (`ghcr.io/acme/app:dev`).
- A file path that doesn't exist says `no tarball at PATH`.

## It picked the wrong platform

muscles reads Linux on your machine's architecture. On an Apple Silicon Mac that's `linux/arm64`. Choose another with `--platform linux/amd64`, or set `platform:` in `.muscles.yaml` so CI and laptops agree.

## The daemon is slow for big images

Reading from the Docker daemon exports the whole image through the Docker API. For large images it's faster to read the same image from its registry, or to `docker save` it once and point muscles at the tarball. Cached layers are never indexed again, but the daemon still exports the image on every run.

## STEP shows `?`

The image has no build history, or fewer history entries than layers (some builders write none). Everything else works.

## Sizes don't match `docker images`

muscles reports the uncompressed bytes of regular files in each layer, and the compressed size from the manifest. `docker images` reports the unpacked size on the daemon's storage driver, which includes filesystem overhead. Both are right about different things.

## The cache is big, or looks wrong

`muscles cache list` shows it; `muscles cache delete` clears it (nothing is lost, the next command reads the layers again). `--fresh` re-indexes for one command. The cache is capped at `cache.max_size` (2 GB).

## No color, or color in my logs

Color is on only when the stream is a terminal, `NO_COLOR` is unset, `TERM` isn't `dumb` and `--no-color` isn't passed. stdout and stderr are checked separately, so `muscles ... > out.txt` still shows progress on the terminal and writes plain text to the file.

## Reporting a bug

If muscles prints `Crash: internal panic`, it's a bug in muscles, not something you did. Open an issue at <https://github.com/rossedman/muscles/issues/new> with the command, the output and `muscles version`.
