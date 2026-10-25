#!/usr/bin/env bash
# Usage: test-install.sh <dist>
# <dist> is a release-build.sh output dir. Serves it with install.sh on 127.0.0.1 in the
# download/<version>/ + latest/download/ layout and runs install.sh under dash with a temp HOME:
# AC4 (default dir, custom AW_BIN_DIR, pinned AW_VERSION, PATH hint) and AC5 (tampered archive,
# missing checksum line, unsupported uname, 404: non-zero, clear message, AW_BIN_DIR untouched).
set -euo pipefail

dist=${1:?usage: test-install.sh <dist>}
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
dist=$(cd "$dist" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/test-install.XXXXXX")
srv=
trap '[[ -z $srv ]] || kill "$srv" 2>/dev/null || true; rm -rf "$tmp"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

ver=$(sed -n 's/^[0-9a-f]*  agentic-warden_\(v[^_]*\)_.*/\1/p' "$dist/checksums.txt" | sort -u)
[[ $ver == v* && $ver != *$'\n'* ]] || fail "cannot derive one version from checksums.txt: [$ver]"

# serve <name>: copies dist into $tmp/<name> in the release layout (callers may then damage it)
serve_dir() {
  local w=$tmp/$1
  mkdir -p "$w/download/$ver" "$w/latest/download"
  cp "$dist"/* "$w/download/$ver/"
  cp "$dist"/* "$w/latest/download/"
  cp "$root/install.sh" "$w/install.sh"
}
start_server() { # $1 = web root; sets base
  : >"$tmp/srv.log"
  python3 -u -m http.server 0 --bind 127.0.0.1 --directory "$1" >"$tmp/srv.log" 2>&1 &
  srv=$!
  local i port=
  for i in $(seq 100); do
    port=$(sed -n 's/.*127\.0\.0\.1 port \([0-9]*\).*/\1/p' "$tmp/srv.log" | head -1)
    [[ -n $port ]] && break
    sleep 0.1
  done
  [[ -n $port ]] || fail "http server did not start"
  base=http://127.0.0.1:$port
}
stop_server() { kill "$srv" 2>/dev/null || true; wait "$srv" 2>/dev/null || true; srv=; }

snap() { # content + hashes of a dir, or ABSENT
  if [[ -d $1 ]]; then (cd "$1" && find . | sort && find . -type f -exec sha256sum {} + | sort -k2); else echo ABSENT; fi
}

home=$tmp/home
mkdir -p "$home"
shim=$tmp/shim
mkdir -p "$shim"
printf '#!/bin/sh\necho Plan9\n' >"$shim/uname"
chmod +x "$shim/uname"

# run_install <web-root> [env assignments...]: output in $out, status in $rc. Always dash, temp HOME.
run_install() {
  local web=$1
  shift
  stop_server 2>/dev/null || true
  start_server "$web"
  rc=0
  out=$(env -i HOME="$home" PATH="$PATH" AW_BASE_URL="$base" "$@" dash "$web/install.sh" 2>&1) || rc=$?
  stop_server
}

# expect_fail <name> <pattern> <web-root> [env...]: non-zero, message matches, bin dir untouched
expect_fail() {
  local name=$1 pat=$2 web=$3
  shift 3
  local bin=$tmp/bin-$name
  mkdir -p "$bin"
  echo old >"$bin/aw"
  local before after
  before=$(snap "$bin")
  run_install "$web" AW_BIN_DIR="$bin" "$@"
  [[ $rc -ne 0 ]] || fail "$name: expected non-zero exit, output: $out"
  grep -Eqi "$pat" <<<"$out" || fail "$name: message does not match /$pat/: $out"
  after=$(snap "$bin")
  [[ $before == "$after" ]] || fail "$name: AW_BIN_DIR changed"
  echo "ok $name"
}

check_installed() { # <bindir>
  local b
  for b in aw aw-policy aw-sync awd; do [[ -x $1/$b ]] || fail "$1/$b missing or not executable"; done
  [[ $("$1/aw" version) == "$ver" ]] || fail "aw version: got [$("$1/aw" version)], want [$ver]"
}

serve_dir good

# AC4: default dir, latest
run_install "$tmp/good" HOME="$home"
[[ $rc -eq 0 ]] || fail "default install failed: $out"
check_installed "$home/.local/bin"
grep -q "$home/.local/bin" <<<"$out" && grep -qi 'PATH' <<<"$out" || fail "no PATH hint in: $out"
grep -Fq "Installed agentic-warden $ver to $home/.local/bin: aw aw-policy aw-sync awd. Run: aw version" <<<"$out" || fail "success line wrong: $out"
echo "ok default dir (latest)"

# AC4: custom dir, pinned version, dir on PATH -> no hint
mkdir -p "$tmp/mybin"
run_install "$tmp/good" AW_BIN_DIR="$tmp/mybin" AW_VERSION="$ver" PATH="$tmp/mybin:$PATH"
[[ $rc -eq 0 ]] || fail "pinned install failed: $out"
check_installed "$tmp/mybin"
! grep -qi 'not on your PATH' <<<"$out" || fail "PATH hint printed though dir is on PATH: $out"
echo "ok custom dir, pinned"

# AC5
serve_dir tampered
for d in download/$ver latest/download; do
  for f in "$tmp"/tampered/$d/agentic-warden_*.tar.gz; do echo x >>"$f"; done
done
expect_fail tampered 'checksum' "$tmp/tampered"

serve_dir nosum
for d in download/$ver latest/download; do
  : >"$tmp/nosum/$d/checksums.txt"
done
expect_fail nosum 'checksum' "$tmp/nosum"

expect_fail uname 'unsupported' "$tmp/good" PATH="$shim:$PATH"
expect_fail notfound 'download failed: .*\(does AW_VERSION=v9\.9\.9 exist\?\)' "$tmp/good" AW_VERSION=v9.9.9

# staging failure on the 3rd binary: nothing in AW_BIN_DIR may change (no mixed versions, no residue)
cshim=$tmp/chmod-shim
mkdir -p "$cshim"
cat >"$cshim/chmod" <<'SH'
#!/bin/sh
n=$(cat "$AW_TEST_COUNT" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" >"$AW_TEST_COUNT"
[ "$n" -ne 3 ] || exit 1
exec /bin/chmod "$@"
SH
/bin/chmod +x "$cshim/chmod"
expect_fail staging 'cannot (write|stage)' "$tmp/good" PATH="$cshim:$PATH" AW_TEST_COUNT="$tmp/chmod-count"

# SIGTERM during staging: the installer must stop and leave AW_BIN_DIR as it was
sshim=$tmp/sig-shim && mkdir -p "$sshim"
printf '%s\n' '#!/bin/sh' 'n=$(cat "$AW_TEST_COUNT" 2>/dev/null || echo 0); echo $((n + 1)) >"$AW_TEST_COUNT"' \
  '[ "$n" -ne 1 ] || kill -TERM "$PPID"' 'exec /bin/chmod "$@"' >"$sshim/chmod"
/bin/chmod +x "$sshim/chmod"
expect_fail signal '' "$tmp/good" PATH="$sshim:$PATH" AW_TEST_COUNT="$tmp/sig-count"
! grep -q cannot <<<"$out" || fail "signal: installer kept running after SIGTERM: $out"

echo "PASS"
