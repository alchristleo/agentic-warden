---
title: Environment variables and files
section: reference
order: 1
description: Every environment variable the four binaries read, and every file they write, per operating system.
---

# Environment variables and files

Defaults come from `awd help`, `aw help` and `aw-sync help`, and from the code that reads them.

## Control plane (`awd`)

| Variable | Purpose |
|---|---|
| `AWD_ADDR` | Listen address. Default `:8080`. |
| `AWD_DATABASE_URL` | Postgres URL. Unset means in-memory storage, which does not survive a restart and suits evaluation only. |
| `AWD_LOG_LEVEL` | `debug`, `info`, `warn` or `error`. Default `info`. |
| `AWD_READ_TIMEOUT` | Per-request read timeout. Default `10s`. |
| `AWD_WRITE_TIMEOUT` | Per-request write timeout. Default `30s`. |
| `AWD_IDLE_TIMEOUT` | Keep-alive idle timeout. Default `120s`. |
| `AWD_SHUTDOWN_TIMEOUT` | How long in-flight requests may drain. Default `30s`. |
| `AWD_ADMIN_TOKEN` | Bearer token for `apply` and machine administration. Unset disables them. |
| `AWD_SCIM_TOKEN` | Bearer token the identity provider presents on `/scim/v2`. Unset disables SCIM. |
| `AWD_SIGNING_KEY` | Path to a signing key written by `awd keygen`, or `awskms:<key ARN>`. Unset means bundles are not signed. |
| `AWD_SIGNING_KEY_PREVIOUS` | The key being rotated out, signed over during a rotation. |
| `AWD_PUBLIC_URL`, `AWD_CONSOLE_*` | Turn on the admin console. All or none; see [Admin console](./admin-console.md). |

## Command-line clients

| Variable | Read by | Purpose |
|---|---|---|
| `AWD_URL` | `awd` subcommands | Control plane URL used by `apply` and the admin commands. Default `http://localhost:8080`. |
| `AWD_ADMIN_TOKEN` | `awd` subcommands | The same token the server uses. |
| `AWD_APPLIED_BY` | `awd apply` | Name recorded in the audit log. Falls back to `$USER`, then `$USERNAME`. |
| `AW_SYNC_TOKEN` | `aw-sync enroll` | The enrollment token, so it need not appear on the command line. |
| `AW_SYNC_STATE_DIR` | `aw` | Where `aw` and `aw doctor` look for aw-sync's state. Overrides the OS default. `aw-sync` itself takes `--state-dir`. |
| `AW_POLICY` | `aw` | Policy document to apply. The `--policy` flag overrides it. |

`AW_POLICY_BUNDLE` and `AW_POLICY_CONFIG` exist only in a helper built with `-tags awtest`, for the
tests. A release build ignores them and says so on stderr. `aw doctor` warns if a tagged helper is
installed.

## Files written on a managed machine

| What | Linux | macOS | Windows |
|---|---|---|---|
| aw-sync state (`machine.json`, `state.json`, `aw-bundle.json`, audit log, lock) | `/var/lib/agent-wrapper` | `/Library/Application Support/agent-wrapper` | `%ProgramData%\agent-wrapper` |
| Claude Code bundle and drop-in | `/etc/claude-code` | `/Library/Application Support/ClaudeCode` | `C:\Program Files\ClaudeCode` |
| Codex `requirements.toml` | `/etc/codex` | `/etc/codex` | `%ProgramData%\OpenAI\Codex` |
| Gemini `settings.json` and `policies/50-agent-wrapper.toml` | `/etc/gemini-cli` | `/Library/Application Support/GeminiCli` | `%ProgramData%\gemini-cli` |
| Helper and sync binaries | `/usr/local/bin` | `/usr/local/bin` | `C:\Program Files\AgentWrapper` |

Under the Claude Code directory, `aw-sync` writes `aw-bundle.json` and
`managed-settings.d/50-agent-wrapper.json`, each with an `.aw-revision` sidecar. Signed deployments
add `aw-bundle.json.sig` and `aw-trust.pub`. `ProgramData` follows the `ProgramData` environment
variable and falls back to `C:\ProgramData`.

`aw-sync install-timer` adds a scheduler entry:

| OS | Entry |
|---|---|
| Linux | `/etc/systemd/system/aw-sync.service` and `aw-sync.timer` |
| macOS | `/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist`, logging to `/Library/Logs/agent-wrapper/aw-sync.log` |
| Windows | the scheduled task `agent-wrapper\aw-sync` |

## Files written by `aw`

`aw` writes the merged settings it hands to the agent under the user cache directory:
`~/.cache/agent-wrapper/<agent>/settings-<hash>.json` on Linux, and the platform's equivalent
elsewhere.

## Files written by the rootless trial

With `--state-dir ~/.aw-try` and `--root claude=~/.aw-try/claude`, everything above moves under
`~/.aw-try`, except the `aw` cache. [Removing Agentic Warden](./removing.md) lists them.

**Bring your rules.** See it against your own policy. [Request a demo](/demo)
