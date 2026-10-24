#!/bin/sh
# Downloads the pinned ble.sh release into docs/tapes/.ble/ (gitignored), checks its sha256, and
# prints the directory to export as BLE_DIR. Nothing is written outside the repository.
# Keep the version and hash in step with .github/workflows/tapes.yml.
set -eu
ver=0.4.0-devel3
sha=c8612ee612bc6b10dbfd6e85c6cbdfd7caf152a12d1f9de22ea0a9d735b3080c
dest=$(cd "$(dirname "$0")/.." && pwd)/.ble
if [ ! -f "$dest/ble-$ver/ble.sh" ]; then
  mkdir -p "$dest"
  curl -fsSL -o "$dest/ble.tar.xz" "https://github.com/akinomyoga/ble.sh/releases/download/v$ver/ble-$ver.tar.xz"
  echo "$sha  $dest/ble.tar.xz" | sha256sum -c - >&2
  tar xJf "$dest/ble.tar.xz" -C "$dest"
fi
echo "$dest/ble-$ver"
