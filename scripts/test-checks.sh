#!/usr/bin/env bash
# Runs check-quickstart.sh and check-links.sh on the fixtures in scripts/testdata and
# asserts their exit codes and messages.
set -uo pipefail

dir=$(cd "$(dirname "$0")" && pwd)
data=$dir/testdata
qs=$dir/check-quickstart.sh
ln=$dir/check-links.sh
out=$(mktemp "${TMPDIR:-/tmp}/test-checks.XXXXXX")
trap 'rm -f "$out"' EXIT
fail=0

# expect <name> <exit code> <text the output must contain, or -> <command...>
expect() {
  local name=$1 want_exit=$2 want_text=$3
  shift 3
  "$@" >"$out" 2>&1
  local got=$?
  if [[ $got -ne $want_exit ]]; then
    echo "FAIL $name: exit $got, want $want_exit"
    sed 's/^/    /' "$out"
    fail=1
  elif [[ $want_text != - ]] && ! grep -F -q -- "$want_text" "$out"; then
    echo "FAIL $name: output lacks '$want_text'"
    sed 's/^/    /' "$out"
    fail=1
  else
    echo "ok   $name"
  fi
}

expect "quickstart: passing file exits 0" 0 - "$qs" "$data/quickstart-pass.md"
expect "quickstart: missing expected output exits 1" 1 - "$qs" "$data/quickstart-missing-output.md"
expect "quickstart: failure names block 1" 1 "block 1" "$qs" "$data/quickstart-missing-output.md"
expect "quickstart: failure names the missing line" 1 "expected-but-absent" "$qs" "$data/quickstart-missing-output.md"
expect "quickstart: failing block exits 1 and names block 1" 1 "block 1 (" "$qs" "$data/quickstart-failing-block.md"
expect "quickstart: failing and-list exits 1 and names block 1" 1 "block 1 (" "$qs" "$data/quickstart-and-fail.md"
expect "quickstart: prefix assignment with command substitution is flagged" 1 "prefix assignment" "$qs" "$data/quickstart-prefix-assign.md"
expect "quickstart: children of a block are killed on exit" 0 - "$qs" "$data/quickstart-orphan.md"
if pgrep -f '^sleep 4242$' >/dev/null; then
  echo "FAIL orphan: sleep 4242 still running after the session"
  pkill -f '^sleep 4242$'
  fail=1
fi
expect "quickstart: skip exits 0 and prints the reason" 0 "needs a logged-in Claude" "$qs" "$data/quickstart-skip.md"
expect "links: passing file exits 0" 0 - "$ln" "$data/quickstart-pass.md"
expect "links: broken relative link exits 1" 1 "does-not-exist.md" "$ln" "$data/links-broken.md"
expect "links: broken /docs link exits 1" 1 "/docs/nope" "$ln" "$data/links-broken.md"

if [[ $fail -ne 0 ]]; then
  echo "test-checks: FAILED"
  exit 1
fi
echo "test-checks: all passed"
