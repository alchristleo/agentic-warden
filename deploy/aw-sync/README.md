# Installing aw-sync

aw-sync runs as root from a timer. Each run fetches this machine's policy
bundle and rewrites every enrolled agent's governance files atomically; a
failed run leaves the files as they were and exits 1 for the timer to retry.

## 1. Mint an enrollment token

On a machine with `AWD_ADMIN_TOKEN`:

    awd enroll-token alice@acme.com --url https://awd.example.com

The token is single-use and expires in 24 hours by default.

## 2. Install the binary and enroll

Before enrolling, install `aw-policy` too: after the first cycle the drop-in
`aw-sync` writes names `/usr/local/bin/aw-policy`
(`C:\Program Files\AgentWrapper\aw-policy.exe` on Windows), and if that
binary is not there every `claude` launch refuses to start. See
`deploy/managed-settings/README.md` for what ends up in Claude Code's system
directory.

Linux and macOS:

    install -m 0755 aw-policy /usr/local/bin/aw-policy
    install -m 0755 aw-sync /usr/local/bin/aw-sync
    AW_SYNC_TOKEN=<token> sudo -E aw-sync enroll --server https://awd.example.com

Windows (elevated): copy `aw-policy.exe` and `aw-sync.exe` to
`C:\Program Files\AgentWrapper\` and run
`aw-sync.exe enroll --server https://awd.example.com --token <token>`.

Windows caveat: `machine.json`'s 0600 mode is a no-op there — Windows has no
POSIX permission bits, so the file is only as protected as the directory it
lives in. Restrict `C:\ProgramData\agent-wrapper` to SYSTEM and
Administrators right after enrolling:

    icacls "C:\ProgramData\agent-wrapper" /inheritance:r /grant:r "SYSTEM:(OI)(CI)F" "BUILTIN\Administrators:(OI)(CI)F"

`--agents claude,codex,gemini` limits the sync to the agents installed here;
the default is every agent this build can render, and the choices are
`claude`, `codex` and `gemini`. List only the agents this machine runs: a file
rendered for an agent that is not installed is noise for whoever audits the
box. Codex's file is `/etc/codex/requirements.toml`
(`%ProgramData%\OpenAI\Codex\requirements.toml` on Windows), and aw-sync owns
it whole: the next cycle overwrites it, so requirements set by hand belong in
the policy, not in that file. Gemini's files are `/etc/gemini-cli/settings.json`,
owned whole, and `/etc/gemini-cli/policies/50-agent-wrapper.toml`, one file in
a directory other administrators may also use
(`/Library/Application Support/GeminiCli/...` on macOS,
`%ProgramData%\gemini-cli\...` on Windows). Gemini ignores the policies
directory unless it is owned by root and not writable by group or others
(`chmod 755`), or on Windows denies write to standard users; aw-sync creates
it that way, so if a policy seems ignored, check the directory's ownership and
mode first. A user can point `GEMINI_CLI_SYSTEM_SETTINGS_PATH` elsewhere, so
put the rules that must hold in `policies` and treat `settings` as defaults
until the launch-time `aw gemini` wrapper pins that variable. Re-enrolling
needs `--force`.

## 3. Install the timer

    sudo aw-sync install-timer

(On Windows, `aw-sync.exe install-timer` from an elevated prompt.) It
refuses a machine that is not enrolled, schedules `aw-sync once` every five
minutes — `--interval 15m` changes that, from 1m to 24h in whole minutes —
and runs the binary you invoked, wherever it is installed. Because the timer
runs that binary as root, on Linux and macOS it refuses unless the binary and
the state directory, and every directory above them, are owned by root and
writable by no one else — install aw-sync somewhere like `/usr/local/bin`,
not a build directory or `~/Downloads`. On machines where `/usr/local/bin`
itself is not root-owned — Homebrew on Intel Macs takes it over — that
refusal fires there too; install aw-sync into a directory root creates
instead, e.g. `sudo install -d -m 0755 /opt/agent-wrapper/bin && sudo install
-m 0755 aw-sync /opt/agent-wrapper/bin/`, and run `install-timer` from that
copy. On Windows it warns when the binary is outside `Program Files`.
Running it again replaces the timer.
`sudo aw-sync uninstall-timer` removes it and leaves the enrollment and the
rendered files alone.

| OS | What it installs |
| --- | --- |
| Linux | `/etc/systemd/system/aw-sync.service` and `aw-sync.timer`, enabled and started |
| macOS | `/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist`, bootstrapped into the system domain; logs to `/Library/Logs/agent-wrapper/aw-sync.log` |
| Windows | the scheduled task `agent-wrapper\aw-sync`, as SYSTEM, every interval and at startup |

### Shipping the units yourself

MDM pipelines that push files rather than run commands can ship the units
in this directory; they are exactly what `install-timer` writes for
`/usr/local/bin/aw-sync` (`C:\Program Files\AgentWrapper\aw-sync.exe`) and a
five-minute interval, and a test keeps them that way.

Linux (systemd):

    install -m 0644 systemd/aw-sync.service systemd/aw-sync.timer /etc/systemd/system/
    systemctl daemon-reload
    systemctl enable --now aw-sync.timer

Linux without systemd has no unit here; run `aw-sync once` from cron every
five minutes instead.

macOS (launchd):

    install -d -m 0755 /Library/Logs/agent-wrapper
    install -m 0644 launchd/com.agent-wrapper.aw-sync.plist /Library/LaunchDaemons/
    launchctl enable system/com.agent-wrapper.aw-sync
    launchctl bootstrap system /Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist

Windows (Task Scheduler), elevated:

    schtasks /Create /TN "agent-wrapper\aw-sync" /XML windows\aw-sync-task.xml /F

This Windows path has not yet been run on a real Windows host. `schtasks` can
be particular about the XML file's encoding; PowerShell (elevated) reads the
file as text and does not care:

    Register-ScheduledTask -TaskName 'aw-sync' -TaskPath '\agent-wrapper\' -Xml (Get-Content -Raw windows\aw-sync-task.xml)

## 4. Verify

    sudo aw-sync once
    aw-sync status

`status` shows the last sync, the bundle version, every rendered file with
`ok`, `drift` or `missing`, and the last error. It runs as any user, so
developers can check it too.

Only one cycle runs at a time: a `once` that starts while another is
running (a timer tick during a manual run) prints
`note: another aw-sync cycle is running; skipped` and exits 0.

## Where things live

| | Linux | macOS | Windows |
| --- | --- | --- | --- |
| State (`machine.json` 0600, `state.json` 0644, `aw-sync-audit.log`, `aw-bundle.json` 0644, the full bundle `aw` compiles per launch) | `/var/lib/agent-wrapper/` | `/Library/Application Support/agent-wrapper/` | `C:\ProgramData\agent-wrapper\` |
| Claude Code | `/etc/claude-code/` | `/Library/Application Support/ClaudeCode/` | `C:\Program Files\ClaudeCode\` |

Rendered files are root-owned and world-readable. A user who can edit them
already has root; `once` rewrites drift unconditionally.

## Signing the bundle

Signing lives on the control plane, not here: `awd keygen --out
/etc/agent-wrapper/signing.key` writes a 0600 Ed25519 seed and prints its
public key and key ID, and refuses to overwrite a key that is already
there. `AWD_SIGNING_KEY` names that file to `awd`, which signs every
bundle it serves from then on; leave it unset and bundles stay unsigned,
which is exactly today's behaviour. `AWD_SIGNING_KEY_PREVIOUS` names the
key being rotated out — set but unreadable, `awd` refuses to start, so a
half-configured rotation can never pass for a finished one. A machine pins
whichever key it saw at enrollment and keeps trusting it across a
rotation, so none of this needs a machine to re-enroll.

Turning signing back off is safe too: unset `AWD_SIGNING_KEY`, re-enroll
the machine so its pin is empty again, and the next cycle removes the
`aw-bundle.json.sig` and `aw-trust.pub` it left behind. Without that
removal `aw-policy` would go on checking every bundle against a key the
control plane no longer signs with, and fail every session.

## Rotating the signing key

1. `awd keygen --out /etc/agent-wrapper/signing-2.key` on the control
   plane.
2. Set `AWD_SIGNING_KEY=/etc/agent-wrapper/signing-2.key` and
   `AWD_SIGNING_KEY_PREVIOUS=/etc/agent-wrapper/signing.key`, and restart
   `awd`. Every already-enrolled machine still verifies against the old
   key, sees the rollover statement `awd` now signs with it, and repins
   itself to the new key on its next cycle — including a cycle the server
   answers 304, which on a fleet whose policy is stable is every cycle.
3. Watch `awd machines` until every row's KEY column shows the new key ID
   — that is what tells you the rotation has actually reached every
   machine, not just the control plane.
4. Drop `AWD_SIGNING_KEY_PREVIOUS`, restart `awd`, and archive the old
   key; nothing verifies against it anymore.

## Revoking a machine

    awd revoke <machine-id> --url https://awd.example.com

The machine's next `once` gets 401, records the error, exits 1, and keeps
its files. Re-enroll it with a new token and `--force`.
