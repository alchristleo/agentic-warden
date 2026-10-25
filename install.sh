#!/bin/sh
# agentic-warden installer. Downloads the release archive for this OS/arch, verifies it against
# the release's checksums.txt and installs aw, aw-policy, aw-sync and awd into AW_BIN_DIR.
# Env: AW_VERSION (default: latest), AW_BIN_DIR (default: $HOME/.local/bin), AW_BASE_URL.
# Never uses sudo. AW_BIN_DIR is untouched until the archive is verified and all four binaries are
# staged; a failure before that leaves it as it was. Only the final four renames could fail midway.
set -eu

base=${AW_BASE_URL:-https://github.com/alchristleo/agentic-warden/releases}
version=${AW_VERSION:-latest}
bindir=${AW_BIN_DIR:-${HOME:?HOME is not set}/.local/bin}

die() { echo "install.sh: $*" >&2; exit 1; }

case $(uname -s) in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) die "unsupported OS '$(uname -s)' (supported: Linux, Darwin)" ;;
esac
case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "unsupported architecture '$(uname -m)' (supported: x86_64, arm64)" ;;
esac

if [ "$version" = latest ]; then
  url=$base/latest/download
else
  url=$base/download/$version
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  die "need sha256sum or shasum to verify the download"
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/aw-install.XXXXXX")
cleanup() {
  rm -rf "$tmp"
  for c in aw aw-policy aw-sync awd; do rm -f "$bindir/.$c.new.$$"; done
}
trap cleanup EXIT
trap 'exit 1' INT TERM

fetch() {
  curl -fsSL -o "$2" "$1" && return 0
  hint=
  [ "$version" = latest ] || hint=" (does AW_VERSION=$version exist?)"
  die "download failed: $1$hint"
}

fetch "$url/checksums.txt" "$tmp/checksums.txt"
line=$(grep "  agentic-warden_[^ /]*_${os}_${arch}\.tar\.gz\$" "$tmp/checksums.txt" | head -n 1) || true
[ -n "$line" ] || die "checksum verification failed: no checksum line for ${os}/${arch} in checksums.txt"
want=${line%%  *}
file=${line#*  }

fetch "$url/$file" "$tmp/$file"
got=$(sha "$tmp/$file")
[ "$got" = "$want" ] || die "checksum verification failed for $file (expected $want, got $got)"

mkdir "$tmp/x"
tar -xzf "$tmp/$file" -C "$tmp/x" || die "cannot extract $file"
for b in aw aw-policy aw-sync awd; do
  [ -f "$tmp/x/$b" ] || die "archive $file has no $b"
done

mkdir -p "$bindir" || die "cannot create $bindir"
for b in aw aw-policy aw-sync awd; do
  if ! { cp "$tmp/x/$b" "$bindir/.$b.new.$$" && chmod 755 "$bindir/.$b.new.$$"; }; then
    die "cannot stage $bindir/$b; nothing was installed"
  fi
done
for b in aw aw-policy aw-sync awd; do
  mv -f "$bindir/.$b.new.$$" "$bindir/$b" || die "cannot write $bindir/$b"
done

installed=${file#agentic-warden_}
installed=${installed%_"${os}_${arch}".tar.gz}
echo "Installed agentic-warden $installed to $bindir: aw aw-policy aw-sync awd. Run: aw version"
case ":$PATH:" in
  *":$bindir:"*) ;;
  *) echo "$bindir is not on your PATH. Add it, e.g.: export PATH=\"$bindir:\$PATH\"" ;;
esac
