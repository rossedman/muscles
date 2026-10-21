#!/usr/bin/env bash
# Install a muscles release for the GitHub Action (action.yml): the version
# asked for, else the action's own tag, else the latest release. Verifies
# the archive against the release's checksums.txt.
set -euo pipefail

repo=rossedman/muscles
tag=${MUSCLES_VERSION:-}
if [[ -z $tag && ${ACTION_REF:-} =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  tag=$ACTION_REF
fi
if [[ -z $tag || $tag == latest ]]; then
  tag=$(gh release view --repo "$repo" --json tagName --jq .tagName)
fi
[[ $tag == v* ]] || tag=v$tag

case "${RUNNER_OS:-$(uname -s)}" in
  Linux) os=linux ;;
  macOS | Darwin) os=darwin ;;
  *) echo "::error title=muscles::the action runs on Linux and macOS runners, not ${RUNNER_OS:-$(uname -s)}"; exit 1 ;;
esac
case "${RUNNER_ARCH:-$(uname -m)}" in
  X64 | x86_64) arch=amd64 ;;
  ARM64 | arm64 | aarch64) arch=arm64 ;;
  *) echo "::error title=muscles::no muscles build for ${RUNNER_ARCH:-$(uname -m)}"; exit 1 ;;
esac

archive="muscles_${tag#v}_${os}_${arch}.tar.gz"
dir="${RUNNER_TEMP:-/tmp}/muscles-$tag"
mkdir -p "$dir"
gh release download "$tag" --repo "$repo" --dir "$dir" --clobber --pattern "$archive" --pattern checksums.txt
(
  cd "$dir"
  grep " $archive\$" checksums.txt > want.txt
  if command -v sha256sum >/dev/null; then sha256sum -c want.txt; else shasum -a 256 -c want.txt; fi
  tar -xzf "$archive" muscles
)
echo "$dir" >> "$GITHUB_PATH"
"$dir/muscles" version
