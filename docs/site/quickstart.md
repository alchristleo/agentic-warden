---
title: Quickstart
section: get-started
order: 2
description: Seven steps, no root. Write a rule, watch it stop your own agent, then edit it and unplug the control plane.
---

# Quickstart

You run a control plane on your laptop, enroll your own machine, and watch
a rule you applied stop Claude Code in one repository while the same request runs in another.
Nothing is written under `/etc`.

You need Linux or macOS, git, curl, and Claude Code installed and logged in. Docker and Go are not needed. Only the Claude commands in steps 6 and 7 need the login. To build from source instead, see [From source](./reference.md#from-source).

Run the steps in one terminal, in order. The control plane from step 2 keeps running in it.

## 1. Install the four binaries

```sh
git clone https://github.com/alchristleo/agentic-warden.git
cd agentic-warden
REL=https://github.com/alchristleo/agentic-warden/releases
curl -fsSL $REL/latest/download/install.sh \
  | sh
export PATH="$HOME/.local/bin:$PATH"
git checkout -q "$(aw version)"
ls ~/.local/bin | grep '^aw'
```

```text
aw
aw-policy
aw-sync
awd
```

The installer prints a hint that `~/.local/bin` is not on your PATH. The `export` line above fixes that. Details are under [Install](./reference.md#install) in the Reference. The clone is for the example policy in later steps, and `git checkout` puts it on the tag you installed so the examples match your binaries.

![Cloning the repository and installing the four binaries](../assets/01-build.gif)

## 2. Start the control plane

```sh
export AWD_ADDR=127.0.0.1:8080
export AWD_ADMIN_TOKEN=$(openssl rand -hex 16)
awd serve &
awd_pid=$!
sleep 1
```

```text
AWD_DATABASE_URL is unset; storing policy in memory, which does not survive a restart
listening on 127.0.0.1:8080
```

`awd` keeps the policy in memory here, so it is for evaluation only. The warnings it prints are
expected: it stores policy in memory, does not sign bundles and leaves the admin console off.

![Starting awd and seeing it listen](../assets/02-serve.gif)

## 3. Apply a policy

The policy is `examples/quickstart.yaml`. It has two rules:

```yaml
version: "quickstart.1"

rules:
  - name: baseline
    agents:
      claude:
        managed:
          permissions:
            deny:
              - Read(./.env)

  - name: payments-repos
    match:
      repos: ["github.com/acme/payments*"]
    agents:
      claude:
        managed:
          permissions:
            deny:
              - Bash(curl *)
```

`baseline` has no `match` block, so it applies everywhere. `payments-repos` applies only to
repositories whose origin matches `github.com/acme/payments*`.

```sh
awd apply examples/quickstart.yaml
```

```text
applied revision quickstart.1 (2 rules)
```

![Applying the two-rule policy](../assets/03-apply.gif)

## 4. Enroll this machine and sync

After these commands, look for the lines below. The machine id and the paths are yours.

```sh
export AW_SYNC_STATE_DIR=~/.aw-try
token=$(awd enroll-token you@acme.com)
AW_SYNC_TOKEN=$token aw-sync enroll --server http://localhost:8080 --agents claude --state-dir ~/.aw-try
aw-sync once --state-dir ~/.aw-try --root claude=$HOME/.aw-try/claude
```

```text
enrolled machine
synced bundle quickstart.1: wrote 3 files
```

`--root claude=DIR` keeps the rendered files under `~/.aw-try` instead of `/etc/claude-code`. That is
what makes this trial rootless. `AW_SYNC_STATE_DIR` tells `aw` where to find the result.

![Enrolling a machine and syncing the bundle](../assets/04-enroll.gif)

## 5. Make two repositories and look at what `aw` compiled

```sh
cd ..
for r in payments-api sandbox; do git init -q $r && git -C $r remote add origin https://github.com/acme/$r; done
echo FAKE=1 > payments-api/.env
echo FAKE=1 > sandbox/.env
cd payments-api
aw doctor
```

```text
evaluation mode: state dir
is not the system default; the warnings below are expected for a rootless trial
bundle quickstart.1, repo github.com/acme/payments-api
policy compiled for github.com/acme/payments-api
```

`aw doctor` prints what `aw` would run, without running the agent. It opens with an
`evaluation mode` line, because a rootless trial leaves the machine-wide files empty and `doctor`
says so. It checks only the agents you enrolled. Abridged from a real run on Linux, with paths shortened (`~` is your home directory). The `warn:`
lines are the rootless trial talking, not a fault.

```console
wrapper: aw
evaluation mode: state dir ~/.aw-try is not the system default; the warnings below are expected for a rootless trial
policy:  ~/.aw-try/aw-bundle.json (bundle quickstart.1, repo github.com/acme/payments-api)
[...]
agent claude
  warn:   no policyHelper in /etc/claude-code: organization policy is not enforced on bare `claude`
[...]
  note:   policy compiled for github.com/acme/payments-api
```

Now read the settings file `aw` will hand to Claude Code:

```sh
settings=$(aw doctor | sed -n 's/^  file: *//p')
cat "$settings"
```

```text
"Read(./.env)"
"Bash(curl *)"
```

The `payments-api` repository gets both denies. `sandbox` would get only the `.env` one.

![Running aw doctor in payments-api](../assets/05-doctor.gif)

## 6. Ask Claude to run curl in both repositories

Same command in both. In `payments-api` the policy denies `curl` even though the command allows it,
because an organization deny overrides a developer's allow. In `sandbox` there is no such deny, so
it runs. The `.env` rule applies in both.

The prompt goes before `--allowedTools`, which takes a list and would otherwise swallow it.

Claude's wording changes from run to run. Below the commands is an example of each output, in
order: in `payments-api` the `curl` command did not run, the `.env` line comes from Claude Code and is
stable, and in `sandbox` the command ran.

<!-- check: skip (needs a logged-in Claude) -->
```sh
aw claude -p "Run: curl -sI https://example.com and show the status line. If a tool is denied, say so in one line and stop." --allowedTools "Bash(curl *)"
aw claude -p "Print the contents of .env"
cd ../sandbox
aw claude -p "Run: curl -sI https://example.com and show the status line. If a tool is denied, say so in one line and stop." --allowedTools "Bash(curl *)"
```

```text
Command didn't run. Permission to run `curl -sI https://example.com` was denied, so I have no status line to show.
File is in a directory that is denied by your permission settings.
Status line:

HTTP/2 200
```

![aw claude refusing curl in payments-api and running it in sandbox](../assets/06-claude.gif)

## 7. Change the rule, then pull the plug

Copy the policy, bump its version and add a second deny to the payments rule:

```sh
cd ../agentic-warden
sed 's/quickstart.1/quickstart.2/' examples/quickstart.yaml > quickstart.2.yaml
echo '              - Bash(wget *)' >> quickstart.2.yaml
awd apply quickstart.2.yaml
aw-sync once --state-dir ~/.aw-try --root claude=$HOME/.aw-try/claude
```

```text
applied revision quickstart.2 (2 rules)
synced bundle quickstart.2: wrote 3 files
```

Look at the new compiled rule in `payments-api`:

```sh
cd ../payments-api
settings=$(aw doctor | sed -n 's/^  file: *//p')
cat "$settings"
```

```text
"Bash(curl *)"
"Bash(wget *)"
```

Now stop the control plane and sync once more:

```sh
kill $awd_pid
wait $awd_pid || true
aw-sync once --state-dir ~/.aw-try --root claude=$HOME/.aw-try/claude || echo "exit status $?"
```

```text
connection refused
exit status 1
```

The sync fails, exits 1 and leaves the files as they were. The policy still applies, because `aw`
compiles from the bundle on local disk:

```sh
settings=$(aw doctor | sed -n 's/^  file: *//p')
cat "$settings"
aw-sync status --state-dir ~/.aw-try
```

```text
"Bash(curl *)"
"Bash(wget *)"
last error: sync: reaching the control plane at http://localhost:8080
```

Run the step 6 command in `payments-api` once more with `awd` stopped. It was recorded that way,
and `curl` is still refused. The policy is a file you own, and it keeps working.

<!-- check: skip (needs a logged-in Claude) -->
```sh
aw claude -p "Run: curl -sI https://example.com and show the status line. If a tool is denied, say so in one line and stop." --allowedTools "Bash(curl *)"
```

![Editing the rule, syncing, then stopping awd](../assets/07-reapply.gif)

To clean up, see
[removing Agentic Warden](./removing.md): for this trial it is `rm -rf ~/.aw-try ~/.cache/agent-wrapper`
and deleting the clone.

## Next

- [Enforce the same rule on plain `claude`](./enforce-plain-claude.md) on a test machine, with sudo.
- [Write your first policy](./write-first-policy.md).
