# Docker plugin

muscles also ships as a Docker CLI plugin, `docker-muscles`, so you can run it as part of `docker`:

```sh
docker muscles app:dev
docker muscles layers list app:dev
docker muscles images diff app:1.8 app:1.9
```

Every command, flag and output is the same as `muscles ...`; `docker muscles layers list app:dev` prints exactly what `muscles layers list app:dev` prints. The plugin's one addition is that it reads images from the daemon of your active [Docker context](#docker-contexts), the way `docker` itself does.

## Install

The plugin is a second binary, `docker-muscles`, released alongside `muscles`.

With Homebrew, the cask installs both binaries:

```sh
brew install rossedman/tap/muscles
```

With Go 1.26 or later:

```sh
go install github.com/rossedman/muscles/cmd/docker-muscles@latest
```

Or download an archive from the [releases page](https://github.com/rossedman/muscles/releases): every archive holds both `muscles` and `docker-muscles`.

## Enable the plugin

Docker looks for plugins named `docker-<name>` in `~/.docker/cli-plugins/`, in its system plugin directories, and in any directory listed under `cliPluginsExtraDirs` in `~/.docker/config.json`. Do one of these:

=== "Link it"

    ```sh
    mkdir -p ~/.docker/cli-plugins
    # Homebrew
    ln -sfn "$(brew --prefix)/bin/docker-muscles" ~/.docker/cli-plugins/docker-muscles
    # go install
    ln -sfn "$(go env GOPATH)/bin/docker-muscles" ~/.docker/cli-plugins/docker-muscles
    ```

=== "Copy it"

    From an unpacked archive:

    ```sh
    mkdir -p ~/.docker/cli-plugins
    install -m 0755 docker-muscles ~/.docker/cli-plugins/
    ```

=== "cliPluginsExtraDirs"

    Add the directory that holds `docker-muscles` to `~/.docker/config.json`, spelled out in full (for Homebrew, the output of `brew --prefix` followed by `/bin`):

    ```json
    {
      "cliPluginsExtraDirs": ["/opt/homebrew/bin"]
    }
    ```

Then check that Docker sees it: `muscles` appears under the plugin commands in `docker --help`, and `docker muscles version` prints the version.

## Docker contexts

Docker doesn't tell plugins which daemon to talk to, so `docker-muscles` works it out the same way `docker` does:

1. If `DOCKER_HOST` is set, it's used as is.
2. Otherwise the context is `DOCKER_CONTEXT`, or else `currentContext` in `config.json` in the Docker config directory (`DOCKER_CONFIG`, or `~/.docker`).
3. For the `default` context (or none), muscles uses Docker's usual default socket.
4. For any other context, muscles reads its endpoint from `contexts/meta/` in the config directory and uses that daemon, with the context's TLS certificates when it has them.

So with Colima, OrbStack, a Docker Desktop context or a remote daemon selected by `docker context use`, `docker muscles layers app:dev` reads `app:dev` from the same daemon `docker images` lists. A context that's selected but missing or broken is an error, rather than a silent fall back to another daemon:

```text
$ docker muscles layers app:dev
docker muscles: docker context "colima": not found (no /home/me/.docker/contexts/meta/…/meta.json; see docker context ls)
```

To point one run at another daemon, set `DOCKER_HOST` (it wins over any context) or `DOCKER_CONTEXT`:

```sh
DOCKER_HOST=tcp://build-box:2375 docker muscles layers app:dev
DOCKER_CONTEXT=colima docker muscles layers app:dev
```

The standalone `muscles` binary doesn't read Docker contexts: it uses `DOCKER_HOST` or the default socket. With a non-default context, either use `docker muscles`, or set `DOCKER_HOST` to the context's endpoint (`docker context inspect --format '{{.Endpoints.docker.Host}}'`).

## Known limitations

- **Help and errors say `muscles`, not `docker muscles`.** The plugin runs the same command tree as `muscles`, so `docker muscles --help` shows `muscles layers list IMAGE` and errors suggest `muscles ...` commands. Prefix them with `docker` (or run `muscles` directly); they behave the same.
- **SSH contexts aren't supported.** A context whose endpoint is `ssh://...` needs Docker's SSH connection helper, which muscles doesn't include. muscles then can't reach that daemon, so a name like `app:dev` is looked up on its registry instead. Run `docker save app:dev -o app.tar` and point muscles at the tarball, or forward the remote socket and set `DOCKER_HOST` to it.
- **No shell completion through `docker`.** `docker muscles <Tab>` doesn't complete; `muscles completion` still sets up completion for `muscles`.
