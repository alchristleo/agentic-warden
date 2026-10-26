#!/usr/bin/env bash
# Checks that every relative link and image path in a markdown file resolves to a file in
# the repo. Absolute /docs and /docs-assets paths map onto docs/site and docs/assets.
# A #fragment must equal the slug of a heading in the target (same rule as web/lib/docs.ts).
# External URLs, anchors on non-markdown targets and other absolute paths are not checked.
set -euo pipefail

file=${1:?usage: check-links.sh <file.md>}
root=$(cd "$(dirname "$0")/.." && pwd)
dir=$(cd "$(dirname "$file")" && pwd)
link_re='\]\(([^)]*)\)'

# resolve <target> prints the repo path to check; returns 1 when the target is not checked.
resolve() {
  case $1 in
    ''|http://*|https://*|mailto:*) return 1 ;;
    /docs-assets/*) echo "$root/docs/assets/${1#/docs-assets/}" ;;
    /docs) echo "$root/docs/site/index.md" ;;
    /docs/*) echo "$root/docs/site/${1#/docs/}.md" ;;
    /*) return 1 ;;
    *) echo "$dir/$1" ;;
  esac
}

# slugs <file.md> prints the heading slugs of h1 to h3, one per line, skipping fenced code.
slugs() {
  local l in=0 text base n
  local -A seen=()
  while IFS= read -r l || [[ -n $l ]]; do
    case $l in '```'*) in=$((1 - in)); continue ;; esac
    [[ $in -eq 1 ]] || [[ ! $l =~ ^#{1,3}[[:space:]]+(.*)$ ]] && continue
    text=${BASH_REMATCH[1]}
    text=$(LC_ALL=C.UTF-8 sed -E 's/`//g; s/\[([^]]*)\]\([^)]*\)/\1/g; s/[*]//g' <<<"$text")
    base=$(LC_ALL=C.UTF-8 sed -E 's/[^[:alnum:] _-]//g; s/ /-/g' <<<"${text,,}")
    n=${seen[$base]:-0}
    seen[$base]=$((n + 1))
    if [[ $n -eq 0 ]]; then echo "$base"; else echo "$base-$n"; fi
  done < "$1"
}

fail=0 line_no=0 in_code=0
while IFS= read -r line || [[ -n $line ]]; do
  line_no=$((line_no + 1))
  case $line in
    '```'*) in_code=$((1 - in_code)); continue ;;
  esac
  if [[ $in_code -eq 1 ]]; then continue; fi
  rest=$line
  while [[ $rest =~ $link_re ]]; do
    target=${BASH_REMATCH[1]}
    rest=${rest#*"${BASH_REMATCH[0]}"}
    target=${target%% *}
    frag=
    [[ $target == *'#'* ]] && frag=${target#*#}
    target=${target%%#*}
    if [[ -z $target ]]; then
      path=$file
    elif path=$(resolve "$target") && [[ ! -e $path ]]; then
      echo "check-links: $file:$line_no: broken link: $target" >&2
      fail=1
      continue
    elif [[ -z ${path:-} ]]; then
      continue
    fi
    if [[ -n $frag && $frag != top && $path == *.md ]] && ! grep -qxF -- "$frag" <(slugs "$path"); then
      echo "check-links: $file:$line_no: broken anchor: #$frag in ${target:-$file}" >&2
      fail=1
    fi
  done
done < "$file"

exit "$fail"
