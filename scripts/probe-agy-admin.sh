#!/usr/bin/env bash
# Local-only probe: runs the installed agy as root in a throwaway container with the given
# admin_settings.json at /etc/antigravity/admin_settings.json (root:root 0644) against a stub
# gateway, then prints what agy did. Needs docker and go. Writes only to a temp dir.
# Usage: scripts/probe-agy-admin.sh <admin.json>
# Env: AGY_BIN (default ~/.local/agent-clis/bin/agy), IMAGE (default debian:bookworm-slim),
#      PROBE_NO_KEY=1 leaves AGY_LLM_GATEWAY_API_KEY unset.
# No Google sign-in or credential file is copied into the container.
set -uo pipefail

[[ $# -eq 1 && -f $1 ]] || { echo "usage: $0 <admin.json>" >&2; exit 2; }
admin=$(readlink -f "$1")
agy=${AGY_BIN:-$HOME/.local/agent-clis/bin/agy}
image=${IMAGE:-debian:bookworm-slim}
dir=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/probe-agy.XXXXXX")
name=probe-agy-$$
cleanup() { docker rm -f "$name" >/dev/null 2>&1; rm -rf "$work"; }
trap cleanup EXIT

CGO_ENABLED=0 go build -o "$work/stub" "$dir/testdata/agy-stub-gateway/main.go" || exit 1
docker run -d --name "$name" --add-host gateway.example.com:127.0.0.1 "$image" sleep 600 >/dev/null || exit 1
docker cp "$agy" "$name:/usr/local/bin/agy" && docker cp "$work/stub" "$name:/usr/local/bin/stub" || exit 1
docker exec "$name" sh -c 'mkdir -p /etc/antigravity /etc/ssl/certs' || exit 1
docker cp "$admin" "$name:/etc/antigravity/admin_settings.json" || exit 1
docker exec "$name" sh -c 'chown root:root /etc/antigravity/admin_settings.json && chmod 0644 /etc/antigravity/admin_settings.json'
docker exec -d "$name" sh -c 'stub -addr :443 -ca-out /etc/ssl/certs/example-ca.pem >/tmp/stub.log 2>&1'
for _ in $(seq 1 50); do
  docker exec "$name" test -s /etc/ssl/certs/example-ca.pem && break
  sleep 0.1
done

keyenv=(-e AGY_LLM_GATEWAY_API_KEY=probe-key)
[[ -n ${PROBE_NO_KEY:-} ]] && keyenv=()
run() {
  echo "== agy $*"
  docker exec -e HOME=/root "${keyenv[@]}" "$name" timeout 60 agy "$@" >"$work/out" 2>"$work/err"
  echo "exit: $?"
  echo "stdout head:"; head -c 600 "$work/out"
  echo "stderr head:"; head -c 600 "$work/err"
}
run models
run -p "say hi"
echo "== agy log lines mentioning admin or gateway"
docker exec "$name" sh -c 'grep -ihE "admin|gateway" /root/.gemini/antigravity-cli/log/* 2>/dev/null | cut -c1-300 | head -15'
echo "== stub received"
docker exec "$name" cat /tmp/stub.log
