#!/usr/bin/env bash
# Runs the sh blocks of a quickstart markdown file, in order, in one bash session with a
# temp HOME and working dir, and checks each text block after a sh block as expected output.
# A sh block preceded by <!-- check: skip (reason) --> is printed and not run.
# Contract:
# - A fenced block tagged sh is run, in order, in one bash session (set -euo pipefail), in a
#   temp HOME and working dir.
# - A text block directly after a sh block (a "You should see:" line may sit between) lists
#   the expected output: every non-empty line must appear as a substring of that block's
#   combined stdout+stderr. A text block that does not follow a checked sh block or a skipped
#   one is an error.
# - An HTML comment <!-- check: skip (reason) --> on the line before a sh block skips it. Skipped
#   blocks are printed with their reason, never silently dropped. Use it only for steps that need
#   a logged-in Claude or root.
# - Background processes started in a block (awd serve &) are killed when the script exits.
set -euo pipefail

file=${1:?usage: check-quickstart.sh <file.md>}
tmp=$(mktemp -d "${TMPDIR:-/tmp}/check-quickstart.XXXXXX")
pid=
cleanup() {
  trap - INT TERM EXIT
  [[ -z $pid ]] || kill -- -"$pid" 2>/dev/null || true
  rm -rf "$tmp"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p "$tmp/home" "$tmp/work" "$tmp/out"
session=$tmp/session.sh
checks=$tmp/checks.tsv
skip_re='^<!-- check: skip \((.*)\) -->$'
# Known gap: the substitution stops at the first ")", so nested parens are not flagged.
assign_re='^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*=\$\([^)]*\)[[:space:]]+[^[:space:]#;]'
: > "$checks"

printf 'set -euo pipefail\nOUT=%q\nn=0\nw=\n' "$tmp/out" > "$session"
cat >> "$session" <<'EOF'
exec 3>&2
trap 'rc=$?; if [[ $rc -ne 0 ]]; then echo "check-quickstart: block $n ($w) failed with exit $rc" >&3; cat "$OUT/$n" >&3 2>/dev/null || true; fi' EXIT
EOF

ran=0 skipped=0 n=0 in_fence=0 tag= run=0 after=0 want=0 sh_line=0 prev= line_no=0
while IFS= read -r line || [[ -n $line ]]; do
  line_no=$((line_no + 1))
  if [[ $in_fence -eq 1 ]]; then
    if [[ $line == '```' ]]; then
      in_fence=0
      if [[ $tag == sh && $run -eq 1 ]]; then
        printf '} > "$OUT/%s" 2>&1\n' "$n" >> "$session"
        after=$n
      fi
    else
      if [[ $tag == sh && $run -eq 1 ]]; then
        if [[ $line =~ $assign_re ]]; then
          echo "check-quickstart: $file:$line_no: prefix assignment with \$(...) hides its failure under set -e; put the assignment on its own line" >&2
          exit 1
        fi
        printf '%s\n' "$line" >> "$session"
      fi
      if [[ $tag == text && $want -gt 0 && -n ${line//[[:space:]]/} ]]; then
        printf '%s\t%s\t%s\n' "$want" "$file:$sh_line" "$line" >> "$checks"
      fi
    fi
    prev=$line
    continue
  fi
  if [[ $line == '```'* ]]; then
    in_fence=1
    tag=${line:3}
    tag=${tag//[[:space:]]/}
    run=0 want=0
    if [[ $tag == text ]]; then
      if [[ $after -eq 0 ]]; then
        echo "check-quickstart: $file:$line_no: text block does not directly follow a checked sh block or a skip" >&2
        exit 1
      fi
      want=$after
    fi
    after=0
    if [[ $tag == sh ]]; then
      n=$((n + 1)) sh_line=$line_no
      if [[ $prev =~ $skip_re ]]; then
        echo "skip: $file:$line_no (block $n): ${BASH_REMATCH[1]}"
        skipped=$((skipped + 1))
        after=-1
      else
        run=1
        ran=$((ran + 1))
        printf 'n=%s\nw=%q\n{\n' "$n" "$file:$line_no" >> "$session"
      fi
    fi
  elif [[ -n ${line//[[:space:]]/} && $line != "You should see:" ]]; then
    after=0
  fi
  prev=$line
done < "$file"

set -m
(cd "$tmp/work" && HOME="$tmp/home" bash "$session") &
pid=$!
status=0
wait "$pid" || status=$?
kill -- -"$pid" 2>/dev/null || true
pid=
[[ $status -eq 0 ]] || exit 1

fail=0 checked=0
while IFS=$'\t' read -r n where text; do
  checked=$((checked + 1))
  if ! grep -F -q -- "$text" "$tmp/out/$n"; then
    echo "check-quickstart: $where (block $n): expected output not found: $text" >&2
    fail=1
  fi
done < "$checks"
[[ $fail -eq 0 ]] || exit 1

echo "check-quickstart: $file ok ($ran run, $skipped skipped, $checked expected lines found)"
