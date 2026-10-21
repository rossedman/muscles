# Releasing

Releases are built by [GoReleaser](https://goreleaser.com) from a version tag. Each one publishes:

- archives for macOS, Linux and Windows on amd64 and arm64, each with the binary, shell completions (bash, zsh, fish) and a man page,
- `checksums.txt`, signed keylessly with cosign (`checksums.txt.sigstore.json`), and build provenance for every archive,
- a multi-arch container image, `ghcr.io/rossedman/muscles:<version>` and `:latest`, signed with cosign, for CI systems that run steps in containers,
- release notes grouped from the merged commits, and
- the GitHub Action at that tag: `uses: rossedman/muscles@vX.Y.Z` installs that release, and
- a Homebrew cask in [rossedman/homebrew-tap](https://github.com/rossedman/homebrew-tap), once the tap is set up (below).

## Cut a release

From an up-to-date `main` with green CI:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The `release` workflow runs `make check`, then GoReleaser, which signs the checksums and image with the workflow's GitHub identity (no keys or secrets), then attaches build provenance. [SECURITY.md](https://github.com/rossedman/muscles/blob/main/SECURITY.md#verifying-a-release) says how users verify them. The release appears on the [releases page](https://github.com/rossedman/muscles/releases) a few minutes later. Versions follow [semver](https://semver.org); while muscles is `0.x`, a minor bump may change behavior, and the notes say how. The JSON schema (`muscles/v1`) only changes by adding fields.

To see what a release would contain without publishing anything:

```bash
make release-snapshot   # builds every archive and image into dist/, unsigned
```

## Set up the Homebrew tap

Until this is done, releases skip the cask and everything else still publishes.

1. Create a public repository named `homebrew-tap` under `rossedman`, with a README.
2. Create a [fine-grained personal access token](https://github.com/settings/personal-access-tokens/new) with access to only that repository and **Contents: read and write**.
3. Add it to this repository as an Actions secret named `HOMEBREW_TAP_GITHUB_TOKEN` (Settings → Secrets and variables → Actions).

The next release commits `Casks/muscles.rb` to the tap, and `brew install rossedman/tap/muscles` works from then on. The cask installs the completions and man page too.

## The container image

The release workflow logs in to `ghcr.io` with the workflow's `GITHUB_TOKEN` (it has `packages: write`). The first push creates the package as private: make it public once under the package's settings on GitHub.

!!! note "macOS Gatekeeper"
    The binaries aren't notarized. The cask clears the quarantine flag after installing, so Homebrew users see no prompt. If you download an archive by hand, run `xattr -d com.apple.quarantine muscles` once, or allow it in System Settings → Privacy & Security.
