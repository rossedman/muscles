# Security

muscles reads container images, which can come from anyone. A way for an
image to make muscles do something it shouldn't is a security bug. Thanks
for reporting one.

## Reporting a vulnerability

Report it privately through GitHub:
**[Report a vulnerability](https://github.com/rossedman/muscles/security/advisories/new)**
(the repository's Security tab → Advisories → Report a vulnerability).
Please don't open a public issue, pull request or discussion for it.

Include what you can of:

- what an attacker controls (an image, a tarball, a registry response, a
  `.muscles.yaml`) and what they get;
- steps or a proof of concept;
- `muscles version`.

You'll get an answer within a week. A fix goes into a new release, and the
advisory is published with it, crediting you unless you'd rather not be.

## Verifying a release

Releases after v0.0.4 are signed keylessly with [cosign](https://docs.sigstore.dev): the
signature binds them to this repository's release workflow, with no keys to
leak. `checksums.txt` lists every archive, and `checksums.txt.sigstore.json`
signs it. Check the signature, then the archive you downloaded:

```bash
VERSION=0.1.0   # the release you downloaded
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/rossedman/muscles/.github/workflows/release.yml@refs/tags/v${VERSION}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --ignore-missing -c checksums.txt
```

The container image is signed the same way:

```bash
cosign verify ghcr.io/rossedman/muscles:0.1.0 \
  --certificate-identity-regexp '^https://github\.com/rossedman/muscles/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Each archive also has [build provenance](https://slsa.dev):
`gh attestation verify muscles_0.1.0_linux_amd64.tar.gz --repo rossedman/muscles`.

## Supported versions

Fixes go into the latest release only.

## What counts

muscles keeps these true; a way to break one is a vulnerability:

- **Images are data.** Nothing from an image (paths, file contents, history,
  labels) is executed, written to disk outside the cache, or used to build a
  command line.
- **Nothing escapes the cache.** The only files muscles writes are layer
  indexes in its cache directory, named by digest.
- **Credentials stay put.** Registry credentials come from the Docker and
  Podman keychains and are never printed, logged or sent anywhere but the
  registry they belong to. Flags never take secrets.
- **No other network.** muscles talks only to the registries named in its
  arguments (and the local Docker daemon). No telemetry.
- **Bounded work.** A malformed image may fail to read, but shouldn't hang
  muscles or exhaust memory out of proportion to its size.
