# Sourced by the tapes inside the recording shell, before Show. Usage: . docs/tapes/lib/prep.sh N
# Runs quickstart steps 1 to N-1 quietly, so tape N starts where a reader is at step N.
# Needs AW_REPO (the checkout the clone in step 1 is taken from). AW_MANUAL=1 keeps your real HOME
# and your real claude (for the tapes that need a logged-in Claude); otherwise HOME is a fresh temp
# dir and a stub claude is put first on PATH. Repo config is set through GIT_CONFIG_COUNT env only.
_aw_n=${1:?step number}
_aw_r() { "$@" >/dev/null 2>&1 || { echo "prep failed: $*"; return 1; }; }

export GOMODCACHE=$(go env GOMODCACHE) GOCACHE=$(go env GOCACHE)
if [ -z "${AW_MANUAL:-}" ]; then
  export HOME=$(mktemp -d)
  mkdir "$HOME/stub" && printf '#!/bin/sh\nexit 0\n' > "$HOME/stub/claude" && chmod +x "$HOME/stub/claude"
  export PATH=$HOME/stub:$PATH
fi
cd "$(mktemp -d)" && mkdir demo && cd demo
export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0="url.$AW_REPO.insteadOf" GIT_CONFIG_VALUE_0=https://github.com/alchristleo/agentic-warden.git
PS1='\W \$ '

if [ "$_aw_n" -gt 1 ]; then
  _aw_r git clone https://github.com/alchristleo/agentic-warden.git &&
  cd agentic-warden &&
  _aw_r go build -o bin/ ./cmd/... &&
  export PATH=$PWD/bin:$PATH || return 1
fi
if [ "$_aw_n" -gt 2 ]; then
  export AWD_ADDR=127.0.0.1:8080
  export AWD_ADMIN_TOKEN=$(openssl rand -hex 16)
  awd serve >/dev/null 2>&1 &
  awd_pid=$!
  sleep 1
fi
if [ "$_aw_n" -gt 3 ]; then _aw_r awd apply examples/quickstart.yaml || return 1; fi
if [ "$_aw_n" -gt 4 ]; then
  export AW_SYNC_STATE_DIR=~/.aw-try
  token=$(awd enroll-token you@acme.com 2>/dev/null)
  AW_SYNC_TOKEN=$token _aw_r aw-sync enroll --server http://localhost:8080 --agents claude --state-dir ~/.aw-try &&
  _aw_r aw-sync once --state-dir ~/.aw-try --root claude=$HOME/.aw-try/claude || return 1
fi
if [ "$_aw_n" -gt 5 ]; then
  cd .. &&
  for r in payments-api sandbox; do git init -q $r && git -C $r remote add origin https://github.com/acme/$r; done &&
  echo FAKE=1 > payments-api/.env &&
  echo FAKE=1 > sandbox/.env &&
  cd payments-api || return 1
fi
unset _aw_n
clear
echo prep done
