# Sourced by the tapes inside the recording shell, before Show. Usage: . docs/tapes/lib/prep.sh N
# Runs quickstart steps 1 to N-1 quietly, so tape N starts where a reader is at step N.
# Needs AW_REPO (the checkout the clone in step 1 is taken from). AW_STUB_BIN (a directory with a
# stub `claude`) is put on PATH when set. AW_MANUAL=1 keeps your real HOME (for the tapes that need
# a logged-in Claude); otherwise HOME is a fresh temp dir.
_aw_n=${1:?step number}
_aw_r() { "$@" >/dev/null 2>&1 || { echo "prep failed: $*"; return 1; }; }

export GOMODCACHE=$(go env GOMODCACHE) GOCACHE=$(go env GOCACHE)
[ -z "${AW_STUB_BIN:-}" ] || export PATH=$AW_STUB_BIN:$PATH
if [ -z "${AW_MANUAL:-}" ]; then export HOME=$(mktemp -d); fi
cd "$(mktemp -d)"
export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0="url.$AW_REPO.insteadOf" GIT_CONFIG_VALUE_0=https://github.com/alchristleo/agentic-warden.git
PS1='$ '

if [ "$_aw_n" -gt 1 ]; then
  _aw_r git clone https://github.com/alchristleo/agentic-warden.git &&
  cd agentic-warden &&
  _aw_r go build -o bin/ ./cmd/... &&
  export PATH=$PWD/bin:$PATH || return 1
fi
if [ "$_aw_n" -gt 2 ]; then
  export AWD_ADMIN_TOKEN=try-me
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
