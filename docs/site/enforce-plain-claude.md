---
title: Enforce on plain claude
section: get-started
order: 3
description: Install the policy helper as root on a test machine, so a developer who types plain claude is governed too. Includes the undo steps.
---

# Enforce on plain claude

The quickstart governs `aw claude`. This page governs plain `claude`. Claude Code runs a managed
setting called `policyHelper` at startup and applies the helper's output as managed settings. The recorded run in
[quickstart](./quickstart.md) step 6, through `aw claude` and `--settings`, shows an organization deny beating a command-line allow; the repository has no recorded run of bare `claude` with the helper. The helper runs on `claude` itself, so a
developer who skips the wrapper is still governed.

Do this on a test VM. It writes under `/etc/claude-code` and `/var/lib/agent-wrapper` and installs
the four binaries into `/usr/local/bin`.

A helper that exits non-zero, or emits settings that fail schema validation, makes Claude Code
refuse to start. On a machine with a pinned signing key, `aw-policy` exits non-zero when the bundle is missing, bad or tampered, unless the organization sets `requireBundle` to `false`. With no pinned key it exits 0 unless `requireBundle` is `true`.

## Install on Linux or macOS

Start from the quickstart's clone, with `awd` running and the policy applied (steps 1 to 3), in the
same shell, so `AWD_ADDR` and `AWD_ADMIN_TOKEN` are still exported. The release binaries from step 1 are the plain ones. Never install a helper built with `-tags awtest`.

First put the binaries in `/usr/local/bin`, owned by root. Download the installer, read it, then run
it with `sudo` (see [Install for enforcement](./reference.md#install-for-enforcement)):

```sh
REL=https://github.com/alchristleo/agentic-warden/releases
curl -fsSLO $REL/latest/download/install.sh
less install.sh
sudo env AW_BIN_DIR=/usr/local/bin sh install.sh
```

Then enroll:

```sh
token=$(awd enroll-token you@acme.com)
AW_SYNC_TOKEN=$token sudo -E aw-sync enroll --server http://localhost:8080 --agents claude
sudo aw-sync once
```

The installer above put `aw-policy` in `/usr/local/bin` before the first sync. The drop-in that `aw-sync` writes names `/usr/local/bin/aw-policy`, and without that binary every `claude` launch refuses to start.

Check it. Without `AW_SYNC_STATE_DIR`, `aw doctor` reads `/var/lib/agent-wrapper`, and the
evaluation-mode line and the root warnings are gone. The quickstart's `awd` does not sign bundles, so
`doctor` still warns "enrolled before bundle signing; re-enroll to pin a key". That is expected here:
the machine pinned no key because the control plane had none.

```sh
unset AW_SYNC_STATE_DIR
cd ../payments-api
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
- `/usr/local/bin/aw`, `aw-policy`, `aw-sync` and `awd`
- `/var/lib/agent-wrapper/`
- the timer units, if you installed them

Then run `awd revoke <machine id>` to revoke the machine's credential on the control plane. The full
list, with macOS and Windows paths, is on [Removing Agentic Warden](./removing.md).
