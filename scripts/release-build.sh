#!/usr/bin/env bash
# Usage: release-build.sh <version> <outdir>
# Builds aw, aw-policy, aw-sync and awd for linux/darwin x amd64/arm64 (static, version-stamped),
# packs one agentic-warden_<version>_<os>_<arch>.tar.gz per target and writes checksums.txt
# (sha256sum format) in <outdir>. Refuses a version not matching ^v[0-9][0-9A-Za-z._+-]*$ and a non-empty outdir.
set -euo pipefail

version=${1:?usage: release-build.sh <version> <outdir>}
outdir=${2:?usage: release-build.sh <version> <outdir>}
[[ $version =~ ^v[0-9][0-9A-Za-z._+-]*$ ]] || { echo "release-build: version must match ^v[0-9][0-9A-Za-z._+-]*\$, got '$version'" >&2; exit 1; }
if [[ -e $outdir && ! -d $outdir ]]; then
  echo "release-build: outdir '$outdir' is not a directory" >&2
  exit 1
fi
if [[ -d $outdir && -n $(ls -A "$outdir") ]]; then
  echo "release-build: outdir '$outdir' is not empty" >&2
  exit 1
fi

root=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$outdir"
outdir=$(cd "$outdir" && pwd)
stage=$(mktemp -d "${TMPDIR:-/tmp}/release-build.XXXXXX")
trap 'rm -rf "$stage"' EXIT

ldflags="-s -w -X github.com/alchristleo/agentic-warden/internal/version.Version=$version"
cd "$root"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*} arch=${target#*/}
  dir=$stage/${os}_$arch
  mkdir -p "$dir"
  for bin in aw aw-policy aw-sync awd; do
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$ldflags" -o "$dir/$bin" "./cmd/$bin"
  done
  cp LICENSE "$dir/LICENSE"
  tar -czf "$outdir/agentic-warden_${version}_${os}_$arch.tar.gz" -C "$dir" aw aw-policy aw-sync awd LICENSE
done

(cd "$outdir" && sha256sum agentic-warden_*.tar.gz >checksums.txt)
