# The policy helper's files

Claude Code runs `aw-policy` at every launch and applies what it prints. The
helper is offline: it reads `aw-bundle.json`, which `aw-sync` keeps up to
date from the control plane. See `deploy/aw-sync/README.md` for installing
`aw-sync`; this page is about what ends up in Claude Code's system
directory and why.

| OS | System directory |
| --- | --- |
| macOS | `/Library/Application Support/ClaudeCode/` |
| Linux and WSL | `/etc/claude-code/` |
| Windows | `C:\Program Files\ClaudeCode\` |

Three files live there:

1. `managed-settings.d/50-agent-wrapper.json` — the drop-in that names the
   helper. `aw-sync` writes it on every cycle; the copies under `unix/` and
   `windows/` here are the same content, for a machine that is provisioned
   by MDM before `aw-sync` first runs, and a test keeps them identical.
2. `aw-bundle.json` — the enrolled user's bundle, written by `aw-sync`,
   mode 0644. It is the organization's policy, not a secret.
3. `aw-policy.json` — the helper's own configuration. Optional. One key:

       {"requireBundle": false}

Plus the `aw-policy` binary at the path the drop-in names:
`/usr/local/bin/aw-policy` on Linux and macOS,
`C:\Program Files\AgentWrapper\aw-policy.exe` on Windows.

Claude Code merges `managed-settings.json` first, then every `*.json` in
`managed-settings.d/` alphabetically. The `50-` prefix leaves room on both
sides for files other teams own. `policyHelper` is honoured only from a file
in that directory, a macOS configuration profile, or the Windows `HKLM`
registry; it is ignored in server-managed settings and in `HKCU`.
`policyHelper.path` must be absolute and normalized: no `.` or `..`
segments, no symlinks. On Windows it must end in `.exe`.

## Installing by hand

    install -m 0755 aw-policy /usr/local/bin/aw-policy
    install -d -m 0755 /etc/claude-code/managed-settings.d
    install -m 0644 unix/managed-settings.d/50-agent-wrapper.json /etc/claude-code/managed-settings.d/
    install -m 0644 aw-policy.json /etc/claude-code/aw-policy.json   # optional

On macOS use `/Library/Application Support/ClaudeCode/` in place of
`/etc/claude-code/`. On Windows copy `aw-policy.exe` to
`C:\Program Files\AgentWrapper\` and `windows\managed-settings.d\50-agent-wrapper.json`
into `C:\Program Files\ClaudeCode\`.

## Timeouts

`timeoutMs` in the drop-in is Claude Code's budget for the whole helper run
(minimum 1000, default 10000). The helper reads one file and does no network
I/O, so the 5000 in the template is generous.

## What happens without a bundle

On a machine `aw-sync` has not reached, or whose bundle does not parse, the
helper emits an envelope with no `managedSettings` and exits 0, which tells
Claude Code to fall back to whatever `managed-settings.json` and the other
drop-ins say. Set `"requireBundle": true` to refuse to start instead; that
is an organization's call, and the default is to keep developers working.
A bundle that compiles to settings this helper's schema rejects is treated
the same way and noted on stderr.

## Verify

On a managed machine, `claude doctor` prints a `Setting sources` line that
must read `(helper)`. `aw doctor` reports the helper, the bundle (present,
version, rule count) and `aw-sync`'s last cycle, and checks for the two
things that silently disable the helper: a server-managed payload from the
claude.ai console, which shadows every file-based source, and a helper path
that does not resolve to an executable.

Break it on purpose once: point `path` at a script that exits 1 and confirm
Claude Code refuses to start. The fail-safe contract is worth seeing rather
than assuming.
