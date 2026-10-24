#!/usr/bin/env bash
# Checks that every relative link and image path in a markdown file resolves to a file in
# the repo. Absolute /docs and /docs-assets paths map onto docs/site and docs/assets.
# External URLs, same-page anchors and other absolute paths are not checked.
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
    target=${target%%#*}
    if path=$(resolve "$target") && [[ ! -e $path ]]; then
      echo "check-links: $file:$line_no: broken link: $target" >&2
      fail=1
    fi
  done
done < "$file"

exit "$fail"
