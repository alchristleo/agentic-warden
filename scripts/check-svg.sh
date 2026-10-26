#!/usr/bin/env bash
# Checks that every tracked SVG parses as XML.
# Usage: check-svg.sh [dir]   (default: the repo root)
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "${1:-$root}"
parse='import sys
import xml.etree.ElementTree as ET
try:
    ET.parse(sys.argv[1])
except ET.ParseError as e:
    print(e)
    sys.exit(1)'
fail=0

while IFS= read -r f; do
  if ! err=$(python3 -I -c "$parse" "$f"); then
    echo "check-svg: $f: $err" >&2
    fail=1
  fi
done < <(git ls-files '*.svg')

exit $fail
