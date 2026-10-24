---
title: Enforce on plain claude
section: get-started
order: 3
description: Install the policy helper as root on a test machine, so a developer who types plain claude is governed too. Includes the undo steps.
---

# Enforce on plain claude

The quickstart governs `aw claude`. This page governs plain `claude`. Claude Code runs a managed
setting called `policyHelper` at startup, and the helper's output becomes the top of the settings
stack, above the command line, the project and the user. The helper runs on `claude` itself, so a
developer who skips the wrapper is still governed.

Do this on a test VM. It writes under `/etc/claude-code` and `/var/lib/agent-wrapper` and installs
two binaries into `/usr/local/bin`.

A helper that exits non-zero, or emits settings that fail schema validation, makes Claude Code
refuse to start. `aw-policy` exits 0 unless the organization sets `requireBundle`.

## Install on Linux or macOS

Start from the quickstart's clone, with `awd` running and the policy applied (steps 1 to 3). Use the
plain binaries from step 1. Never install a helper built with `-tags awtest`.

```sh
token=$(awd enroll-token you@acme.com)
sudo install -m 0755 bin/aw-policy /usr/local/bin/aw-policy
sudo install -m 0755 bin/aw-sync /usr/local/bin/aw-sync
AW_SYNC_TOKEN=$token sudo -E aw-sync enroll --server http://localhost:8080 --agents claude
sudo aw-sync once
```

Install `aw-policy` before the first sync. The drop-in that `aw-sync` writes names
`/usr/local/bin/aw-policy`, and without that binary every `claude` launch refuses to start.

Check it. Without `AW_SYNC_STATE_DIR`, `aw doctor` reads `/var/lib/agent-wrapper`, and the
evaluation-mode line and the root warnings are gone:

```sh
unset AW_SYNC_STATE_DIR
cd payments-api
aw doctor
```

Then ask plain `claude`:

```sh
claude -p "Run: curl -sI https://example.com" --allowedTools "Bash(curl *)"
```

Expect a refusal like quickstart step 6, this time from plain `claude`. This repository has no
recording of a plain `claude` run yet, so check the wording on your machine.

## Pull the plug

Stop `awd` and ask again. `aw-policy` reads the bundle on local disk and needs no server, so the rule
still applies. `sudo aw-sync once` exits 1 and leaves the files as they were.

## Keep it current

`sudo aw-sync install-timer` runs `aw-sync once` every five minutes (`--interval 15m` changes that).
On Linux it writes `/etc/systemd/system/aw-sync.service` and `aw-sync.timer`. On macOS it writes
`/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist`. Because the timer runs the binary as root,
`install-timer` refuses a binary or state directory that anyone but root could replace. More in
[deploy/aw-sync/README.md](https://github.com/alchristleo/agentic-warden/blob/main/deploy/aw-sync/README.md).

## Undo

No uninstall command exists yet. `sudo aw-sync uninstall-timer` stops the timer only; enrollment and
files stay. To remove a root setup by hand on Linux, delete:

- `/etc/claude-code/managed-settings.d/50-agent-wrapper.json` and `/etc/claude-code/aw-bundle.json`.
  The drop-in is what makes plain `claude` call the helper, so delete it first.
- `/usr/local/bin/aw-policy` and `/usr/local/bin/aw-sync`
- `/var/lib/agent-wrapper/`
- the timer units, if you installed them

Then run `awd revoke <machine id>` to revoke the machine's credential on the control plane. The full
list, with macOS and Windows paths, is on [Removing Agentic Warden](./removing.md).

**Bring your rules.** See it against your own policy. [Request a demo](/demo)
