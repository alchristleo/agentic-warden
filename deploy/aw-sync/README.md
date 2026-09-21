# Installing aw-sync

aw-sync runs as root from a timer. Each run fetches this machine's policy
bundle and rewrites every enrolled agent's governance files atomically; a
failed run leaves the files as they were and exits 1 for the timer to retry.

## 1. Mint an enrollment token

On a machine with `AWD_ADMIN_TOKEN`:

    awd enroll-token alice@acme.com --url https://awd.example.com

The token is single-use and expires in 24 hours by default.

## 2. Install the binary and enroll

Linux and macOS:

    install -m 0755 aw-sync /usr/local/bin/aw-sync
    AW_SYNC_TOKEN=<token> sudo -E aw-sync enroll --server https://awd.example.com

Windows (elevated): copy `aw-sync.exe` to `C:\Program Files\AgentWrapper\`
and run `aw-sync.exe enroll --server https://awd.example.com --token <token>`.

Windows caveat: `machine.json`'s 0600 mode is a no-op there — Windows has no
POSIX permission bits, so the file is only as protected as the directory it
lives in. Restrict `C:\ProgramData\agent-wrapper` to SYSTEM and
Administrators right after enrolling:

    icacls "C:\ProgramData\agent-wrapper" /inheritance:r /grant:r "SYSTEM:(OI)(CI)F" "BUILTIN\Administrators:(OI)(CI)F"

`--agents claude` limits the sync to the agents installed here; the default
is every agent this build can render. Codex and Gemini renderers arrive in a
later milestone, so `claude` is the only agent this build can actually sync
today. Re-enrolling needs `--force`.

## 3. Install the timer

Linux (systemd):

    install -m 0644 systemd/aw-sync.service systemd/aw-sync.timer /etc/systemd/system/
    systemctl daemon-reload
    systemctl enable --now aw-sync.timer

macOS (launchd):

    install -d -m 0755 /Library/Logs/agent-wrapper
    install -m 0644 launchd/com.agent-wrapper.aw-sync.plist /Library/LaunchDaemons/
    launchctl bootstrap system /Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist

Windows (Task Scheduler), from an elevated PowerShell:

    .\windows\register-task.ps1

## 4. Verify

    sudo aw-sync once
    aw-sync status

`status` shows the last sync, the bundle version, every rendered file with
`ok`, `drift` or `missing`, and the last error. It runs as any user, so
developers can check it too.

## Where things live

| | Linux | macOS | Windows |
| --- | --- | --- | --- |
| State (`machine.json` 0600, `state.json` 0644, `aw-sync-audit.log`) | `/var/lib/agent-wrapper/` | `/Library/Application Support/agent-wrapper/` | `C:\ProgramData\agent-wrapper\` |
| Claude Code | `/etc/claude-code/` | `/Library/Application Support/ClaudeCode/` | `C:\Program Files\ClaudeCode\` |

Rendered files are root-owned and world-readable. A user who can edit them
already has root; `once` rewrites drift unconditionally.

## Revoking a machine

    awd revoke <machine-id> --url https://awd.example.com

The machine's next `once` gets 401, records the error, exits 1, and keeps
its files. Re-enroll it with a new token and `--force`.
