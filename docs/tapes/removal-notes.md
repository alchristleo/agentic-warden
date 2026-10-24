# Removal notes (read from code; nothing here was run as root)

## Rootless trial (quickstart steps 1 to 7)
Written by `aw-sync enroll` and `aw-sync once --state-dir ~/.aw-try --root claude=~/.aw-try/claude`
(observed in capture.log):
- `~/.aw-try/`: `machine.json`, `state.json`, `aw-bundle.json`, `aw-sync-audit.log`, `aw-sync.lock`
- `~/.aw-try/claude/`: `aw-bundle.json`, `managed-settings.d/50-agent-wrapper.json`
- each rendered file has a `.aw-revision` sidecar
`aw` also writes a merged settings cache at `~/.cache/agent-wrapper/claude/settings-<hash>.json`.
`awd serve` is in-memory: nothing on disk. The clone and `bin/` hold the binaries.
Removal: stop `awd`; `rm -rf ~/.aw-try ~/.cache/agent-wrapper`; delete the clone. Nothing else was written.
Claude does not read `~/.aw-try/claude`; only `aw claude` (via `--settings`) applies the policy in this mode.

## Root setup ("Enforce on bare claude")
Paths from `SystemDir`, `StateDir` and `timer.go` (Linux; macOS and Windows differ):
- Claude: `/etc/claude-code/aw-bundle.json` and `/etc/claude-code/managed-settings.d/50-agent-wrapper.json` (names `policyHelper` = `/usr/local/bin/aw-policy`)
- Codex, if enrolled: `/etc/codex/requirements.toml`
- Gemini, if enrolled: `/etc/gemini-cli/settings.json` and `/etc/gemini-cli/policies/50-agent-wrapper.toml`
- State: `/var/lib/agent-wrapper/` (`machine.json` private, `state.json`, `aw-bundle.json`, audit log, lock)
- Timer (`install-timer`): `/etc/systemd/system/aw-sync.service` and `aw-sync.timer`; on macOS `/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist` and `/Library/Logs/agent-wrapper`
- Binaries, from the install procedure: `/usr/local/bin/aw-policy` and `aw-sync`

## What removing means
No uninstall command exists (README and runbooks have none). `uninstall-timer` stops the timer only;
its own help says "enrollment and files stay". Rendered files are never deleted on failure, by design.
Undo for a root setup = `sudo aw-sync uninstall-timer`, then delete the files above (the
managed-settings drop-in is what makes bare `claude` call the helper), then optionally
`awd revoke <id>` for the machine. The undo page must list these by hand until an uninstall command exists.
