#!/usr/bin/env bash
# Builds a release with release-build.sh v0.0.0-test into a temp dir and checks the result:
# 4 archives, 5 entries each, right OS/arch per binary, checksums, version stamping, and
# that a version not starting with v<digit> is refused.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/test-release-build.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
out=$tmp/out
ver=v0.0.0-test

fail() { echo "FAIL: $*" >&2; exit 1; }

"$here/release-build.sh" "$ver" "$out"

want_archives=$(for p in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
  echo "agentic-warden_${ver}_$p.tar.gz"; done | sort)
got_archives=$(cd "$out" && ls -1 | grep -v '^checksums.txt$' | sort)
[[ $got_archives == "$want_archives" ]] || fail "archives: got [$got_archives], want [$want_archives]"

want_entries=$(printf '%s\n' LICENSE aw aw-policy aw-sync awd | sort)
for p in linux_amd64:"ELF.*x86-64" linux_arm64:"ELF.*aarch64" darwin_amd64:"Mach-O.*x86_64" darwin_arm64:"Mach-O.*arm64"; do
  plat=${p%%:*} pat=${p#*:}
  tgz=$out/agentic-warden_${ver}_$plat.tar.gz
  got=$(tar -tzf "$tgz" | sort)
  [[ $got == "$want_entries" ]] || fail "$plat entries: got [$got]"
  mkdir -p "$tmp/x/$plat"
  tar -xzf "$tgz" -C "$tmp/x/$plat"
  for b in aw aw-policy aw-sync awd; do
    desc=$(file -b "$tmp/x/$plat/$b")
    [[ $desc =~ $pat ]] || fail "$plat/$b: file says '$desc', want /$pat/"
  done
done

(cd "$out" && sha256sum -c checksums.txt >/dev/null) || fail "sha256sum -c checksums.txt"
[[ $(wc -l <"$out/checksums.txt") -eq 4 ]] || fail "checksums.txt should have 4 lines"

bi=$("$tmp/x/linux_amd64/aw-policy" build-info)
grep -qx 'env-overrides=off' <<<"$bi" || fail "build-info lacks env-overrides=off: $bi"
grep -qx "version $ver" <<<"$bi" || fail "build-info lacks 'version $ver': $bi"
[[ $("$tmp/x/linux_amd64/aw" version) == "$ver" ]] || fail "aw version is not $ver"

if "$here/release-build.sh" 0.1.0 "$tmp/bad" 2>/dev/null; then fail "version 0.1.0 accepted"; fi

echo "ok: release build"
