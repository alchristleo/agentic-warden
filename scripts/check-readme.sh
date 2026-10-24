#!/usr/bin/env bash
# Checks that a README renders cleanly on GitHub and keeps its first-screen shape.
# - Posts the file to GitHub's POST /markdown (gfm) and fails if the HTML has a literal "**",
#   a paragraph that starts with "#", or an <img>/<source> whose repo-relative path is missing
#   (paths resolve against the README's directory).
# - Fails unless the hero GIF appears before line 20 and the H2 headings are exactly the
#   expected ones, in order.
# Auth: `gh api` when gh is logged in, else curl with GITHUB_TOKEN (CI passes github.token).
set -euo pipefail

file=${1:-README.md}
dir=$(dirname "$file")
fail=0
err() { echo "check-readme: $*" >&2; fail=1; }

if gh auth status >/dev/null 2>&1; then
  html=$(gh api -X POST /markdown -f mode=gfm -f context=alchristleo/agentic-warden -F "text=@$file")
else
  : "${GITHUB_TOKEN:?check-readme: gh is unavailable or unauthenticated, and GITHUB_TOKEN is unset}"
  html=$(jq -n --rawfile t "$file" '{text: $t, mode: "gfm", context: "alchristleo/agentic-warden"}' |
    curl -fsS -X POST https://api.github.com/markdown \
      -H "Authorization: Bearer $GITHUB_TOKEN" -H "Accept: application/vnd.github+json" --data @-)
fi

if grep -qF '**' <<<"$html"; then err "rendered HTML contains a literal **"; fi
if grep -qE '(^|<p[^>]*>)[[:space:]]*#' <<<"$html"; then err "rendered HTML has text starting with # (a heading that did not render)"; fi

while IFS= read -r p; do
  [[ -z $p || $p == http* || $p == //* || $p == '#'* ]] && continue
  [[ -e $dir/$p ]] || err "missing image path: $p"
done < <(grep -oE '(src|srcset)="[^"]*"' <<<"$html" | sed -E 's/^[a-z]+="//; s/"$//')

hero=$(grep -n 'hero\.gif' "$file" | head -1 | cut -d: -f1 || true)
[[ -n $hero && $hero -lt 20 ]] || err "hero.gif is not before line 20 (found: ${hero:-none})"

want=$'The problem\nSee it in 30 seconds\nHow it works\nWhy teams pick it\nHow it compares\nQuestions your security team will ask\nTry it'
got=$(grep '^## ' "$file" | sed 's/^## //' || true)
[[ $got == "$want" ]] || err $'H2 headings differ from the expected order.\nwant:\n'"$want"$'\ngot:\n'"$got"

[[ $fail -eq 0 ]] || exit 1
echo "check-readme: $file ok"
