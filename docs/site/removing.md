---
title: Removing Agentic Warden
section: security-review
order: 4
description: What Agentic Warden writes to a machine and how to take it off by hand. There is no uninstall command yet.
---

# Removing Agentic Warden

**There is no uninstall command yet.** Neither the README nor the runbooks describe one, and `aw-sync uninstall-timer` stops the timer only: its own help says "enrollment and files stay". Removal today is by hand. This page lists what is written so you can do it. The paths come from reading the code (`SystemDir`, `StateDir` and `internal/sync/timer`); the rootless trial list was observed in a run. Nothing here was run as root.

## After the rootless trial (quickstart)

The trial writes under your home directory and nothing else:

- `~/.aw-try/`: `machine.json`, `state.json`, `aw-bundle.json`, `aw-sync-audit.log`, `aw-sync.lock`
- `~/.aw-try/claude/`: `aw-bundle.json` and `managed-settings.d/50-agent-wrapper.json`
- a `.aw-revision` file beside each rendered `.json` file
- `~/.cache/agent-wrapper/claude/settings-<hash>.json`, the merged settings cache `aw` writes
- the two repositories `../payments-api` and `../sandbox` from step 5

`awd serve` keeps its policy in memory and writes nothing to disk. The binaries are in `~/.local/bin` (`aw`, `aw-policy`, `aw-sync`, `awd`), and `quickstart.2.yaml` from step 7 sits in the clone.

To remove it:

1. Stop `awd`.
2. From inside the clone, run `rm -rf ~/.aw-try ~/.cache/agent-wrapper ../payments-api ../sandbox`.
3. Delete the clone, and delete the four binaries: `rm ~/.local/bin/aw ~/.local/bin/aw-policy ~/.local/bin/aw-sync ~/.local/bin/awd`.

Claude Code does not read `~/.aw-try/claude`. In this mode only `aw claude`, through `--settings`, applies the policy, so nothing else changes while it is there.

## After a root setup ("Enforce on bare claude")

These are the Linux paths. macOS and Windows differ.

| What | Where |
|---|---|
| Claude bundle and drop-in | `/etc/claude-code/aw-bundle.json` and `/etc/claude-code/managed-settings.d/50-agent-wrapper.json` |
| Codex, if enrolled | `/etc/codex/requirements.toml` |
| Gemini (no longer written; aw-sync removes these if unchanged) | `/etc/gemini-cli/settings.json` and `/etc/gemini-cli/policies/50-agent-wrapper.toml` |
| agy, if enrolled | `/etc/antigravity/admin_settings.json` and its `.aw-revision` sibling |
| Helper settings, if you created them | `/etc/claude-code/aw-policy.json` (for example `requireBundle`) |
| Helper audit log, one per developer | `~/.cache/agent-wrapper/aw-policy/aw-policy.log`; on macOS `~/Library/Caches/agent-wrapper/aw-policy/aw-policy.log` |
| State | `/var/lib/agent-wrapper/`: `machine.json` (private), `state.json`, `aw-bundle.json`, the audit log, the lock |
| Timer (`install-timer`) | `/etc/systemd/system/aw-sync.service` and `aw-sync.timer`; on macOS `/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist` and `/Library/Logs/agent-wrapper` |
| Binaries | `/usr/local/bin/aw`, `aw-policy`, `aw-sync` and `awd` |

The drop-in `50-agent-wrapper.json` is what makes bare `claude` run the helper. Delete it, or the binary it names, in the wrong order and you can break launches: if the drop-in stays and `aw-policy` is gone, every `claude` launch refuses to start (`deploy/aw-sync/README.md`). Remove the drop-in first.

To remove it:

1. Run `sudo aw-sync uninstall-timer`. It stops the timer and removes the unit files (`TestUninstallLinux` in `internal/sync/timer/install_test.go`). Enrollment, state and the rendered files stay.
2. Delete `/etc/claude-code/managed-settings.d/50-agent-wrapper.json`, then `/etc/claude-code/aw-bundle.json`, `/etc/claude-code/aw-policy.json` if you created it, and the other rendered files for each agent you enrolled. `aw-sync` writes an `.aw-revision` file beside each rendered `.json` file; delete those too. Signed deployments also leave `aw-bundle.json.sig` and `aw-trust.pub` in the state directory, which step 3 removes.
3. Delete `/var/lib/agent-wrapper/`.
4. Delete `/usr/local/bin/aw`, `aw-policy`, `aw-sync` and `awd`.
5. On each developer account: `rm -rf ~/.cache/agent-wrapper` (on macOS, `~/Library/Caches/agent-wrapper`).
6. Optionally, revoke the machine on the control plane with `awd revoke <id>`, so its credential stops working.

Rendered files are never deleted when a sync fails. That is on purpose: a failed sync leaves the last policy in force (`TestRunKeepsFilesAndStateWhenTheServerFails` in `internal/sync/sync_test.go`). It also means stopping `awd` does not remove any rule.

## Not covered

- We checked the lists against the code and a rootless run, not against a root install on a clean machine.
- A hosted cell is removed by us on your written request. The steps are in the [trust statement](./trust.md).
- When an uninstall command exists, this page will say so.
