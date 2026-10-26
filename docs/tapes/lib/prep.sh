# Sourced by the tapes inside the recording shell, before Show. Usage: . docs/tapes/lib/prep.sh N
# Runs quickstart steps 1 to N-1 quietly, so tape N starts where a reader is at step N.
# Optional BLE_DIR (see docs/tapes/README.md) turns on ble.sh command highlighting.
# Needs AW_REPO (the checkout the clone in step 1 is taken from). AW_MANUAL=1 keeps your real HOME
# and your real claude (for the tapes that need a logged-in Claude); otherwise HOME is a fresh temp
# dir and a stub claude is put first on PATH. Repo config is set through GIT_CONFIG_COUNT env only.
_aw_n=${1:?step number}
_aw_r() { "$@" >/dev/null 2>&1 || { echo "prep failed: $*"; return 1; }; }

export GOMODCACHE=$(go env GOMODCACHE) GOCACHE=$(go env GOCACHE)
if [ -z "${AW_MANUAL:-}" ]; then
  # A fixed path, so that the recordings show /tmp/aw-home and not a random /tmp/tmp.XXXXXXXXXX.
  rm -rf /tmp/aw-home && mkdir /tmp/aw-home && export HOME=/tmp/aw-home
  mkdir "$HOME/stub" && printf '#!/bin/sh\nexit 0\n' > "$HOME/stub/claude" && chmod +x "$HOME/stub/claude"
  export PATH=$HOME/stub:$PATH
fi
cd "$(mktemp -d)" && mkdir demo && cd demo
export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0="url.$AW_REPO.insteadOf" GIT_CONFIG_VALUE_0=https://github.com/alchristleo/agentic-warden.git
PS1='\[\e[34m\]\W\[\e[0m\] \[\e[32m\]\$\[\e[0m\] '

if [ "$_aw_n" -eq 1 ]; then
  # Step 1 installs from a release. Serve a build of the checkout on localhost instead (as
  # scripts/check-quickstart.sh does with AW_QS_DIST): install.sh reads AW_BASE_URL, and a curl
  # function rewrites the GitHub releases URL the reader types. HOME is the temp dir above, so the
  # install lands in a throwaway ~/.local/bin. aw_srv is the server pid; the tape kills it at the end.
  [ -z "${AW_MANUAL:-}" ] || { echo "prep: tape 1 needs the temp HOME, unset AW_MANUAL"; return 1; }
  _aw_r "$AW_REPO/scripts/release-build.sh" v0.0.0-ci "$HOME/dist" || return 1
  # The installed version is v0.0.0-ci, which has no tag: the tape's clone comes from a tagged copy,
  # so that the visible "git checkout" succeeds.
  _aw_r git clone --bare "$AW_REPO" "$HOME/clone.git" && _aw_r git -C "$HOME/clone.git" tag v0.0.0-ci HEAD || return 1
  export GIT_CONFIG_KEY_0="url.$HOME/clone.git.insteadOf"
  _aw_w=$HOME/web
  mkdir -p "$_aw_w/download/v0.0.0-ci" "$_aw_w/latest/download"
  for _aw_d in download/v0.0.0-ci latest/download; do cp "$HOME"/dist/* "$AW_REPO/install.sh" "$_aw_w/$_aw_d/"; done
  python3 -u -m http.server 0 --bind 127.0.0.1 --directory "$_aw_w" >"$HOME/srv.log" 2>&1 &
  aw_srv=$!
  trap 'kill $aw_srv 2>/dev/null' EXIT
  for _aw_i in $(seq 100); do
    _aw_p=$(sed -n 's/.*127\.0\.0\.1 port \([0-9]*\).*/\1/p' "$HOME/srv.log" | head -1)
    [ -n "$_aw_p" ] && break
    sleep 0.1
  done
  [ -n "$_aw_p" ] || { echo "prep: http server did not start"; kill $aw_srv 2>/dev/null; return 1; }
  export AW_BASE_URL=http://127.0.0.1:$_aw_p
  curl() { local a=(); for _aw_x in "$@"; do a+=("${_aw_x/#https:\/\/github.com\/alchristleo\/agentic-warden\/releases/$AW_BASE_URL}"); done; command curl "${a[@]}"; }
  unset _aw_w _aw_d _aw_i _aw_p
fi
if [ "$_aw_n" -gt 1 ]; then
  _aw_r git clone https://github.com/alchristleo/agentic-warden.git &&
  cd agentic-warden &&
  _aw_r go build -o bin/ ./cmd/... &&
  export PATH=$PWD/bin:$PATH || return 1
fi
if [ "$_aw_n" -gt 2 ]; then
  export AWD_ADDR=127.0.0.1:8080
  export AWD_ADMIN_TOKEN=$(openssl rand -hex 16)
  # The previous tape's awd can still hold the port for a moment after its recording ends.
  for _aw_i in $(seq 50); do (exec 3<>/dev/tcp/127.0.0.1/8080) 2>/dev/null || break; sleep 0.2; done
  unset _aw_i
  awd serve >/dev/null 2>&1 &
  awd_pid=$!
  # Out of the job table, so that killing awd prints no "Done ... (wd: /tmp/tmp.XXXXXXXXXX/...)" notice.
  disown
  # disown also keeps awd alive when the recording shell exits; stop it then, or the next tape finds the port taken.
  trap 'kill "$awd_pid" 2>/dev/null' EXIT
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
if [ -n "${BLE_DIR:-}" ]; then
  source "$BLE_DIR/ble.sh" --attach=prompt --norc
  bleopt prompt_eol_mark=''
  for _aw_f in syntax_varname varname_array varname_empty varname_export varname_expr varname_hash varname_number varname_readonly varname_transform varname_unset; do ble-face "$_aw_f=fg=blue"; done
  unset _aw_f
  # An unclosed "$(" and the word typed after it are a syntax error until ")" arrives. Paint them plain, not red.
  ble-face syntax_error=none
  # Builtins such as wait and echo default to red, which reads as an error; paint them like other commands.
  ble-face command_builtin=fg=green
  # The default comment colour (256-colour 242, #6c6c6c) is 3.1:1 on #1e1e2e. 246 (#949494) is 5.4:1.
  ble-face syntax_comment=fg=246
  ble-bind -f RET accept-line
  ble-bind -f C-m accept-line
fi
clear
echo prep done
