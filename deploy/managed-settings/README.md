# Installing the policy helper

Two files reach every machine, through whatever already manages it (Jamf,
Intune, Group Policy, Ansible, a golden image):

1. A drop-in that names the helper, in Claude Code's `managed-settings.d/`.
2. The helper's own configuration, `aw-policy.json`, beside it.

Plus the `aw-policy` binary itself at the path the drop-in names.

| OS | System directory |
| --- | --- |
| macOS | `/Library/Application Support/ClaudeCode/` |
| Linux and WSL | `/etc/claude-code/` |
| Windows | `C:\Program Files\ClaudeCode\` |

Claude Code merges `managed-settings.json` first, then every `*.json` in
`managed-settings.d/` alphabetically. The `50-` prefix leaves room on both
sides for files other teams own. `policyHelper` is honoured only from a file
in that directory, a macOS configuration profile, or the Windows `HKLM`
registry; it is ignored in server-managed settings and in `HKCU`.

## Linux, WSL, macOS

    install -m 0755 aw-policy /usr/local/bin/aw-policy
    install -d -m 0755 /etc/claude-code/managed-settings.d
    install -m 0644 unix/managed-settings.d/50-agent-wrapper.json /etc/claude-code/managed-settings.d/
    install -m 0644 aw-policy.json /etc/claude-code/aw-policy.json

On macOS use `/Library/Application Support/ClaudeCode/` in place of
`/etc/claude-code/`. `policyHelper.path` must be absolute and normalized: no
`.` or `..` segments, no symlink games.

## Windows

Copy `aw-policy.exe` to `C:\Program Files\AgentWrapper\`, then
`windows\managed-settings.d\50-agent-wrapper.json` and `aw-policy.json` into
`C:\Program Files\ClaudeCode\`. The path must end in `.exe`.

## Timeouts

`timeoutMs` in the drop-in is Claude Code's budget for the whole helper run
(minimum 1000, default 10000). `timeoutMs` in `aw-policy.json` is the helper's
own budget for reaching the control plane and must be smaller, leaving room to
read the cache and print. The defaults (5000 and 3000) do that.

## What happens when the control plane is down

The helper serves the last policy it fetched for this user and repository,
and exits 0. On a machine that has never fetched one, it emits an envelope
with no `managedSettings`, which tells Claude Code to fall back to whatever
`managed-settings.json` and the other drop-ins say. Set `"requireFresh": true`
to refuse to start instead; that is an organization's call, and the default
is to keep developers working.

## Verify

On a managed machine, `claude doctor` prints a `Setting sources` line that
must read `(helper)`. `aw doctor` checks for the two things that silently
disable the helper: a server-managed payload from the claude.ai console,
which shadows every file-based source, and a helper path that does not
resolve to an executable.

Break it on purpose once: point `path` at a script that exits 1 and confirm
Claude Code refuses to start. The fail-safe contract is worth seeing rather
than assuming.
