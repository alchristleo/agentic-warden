---
title: Environment variables and files
section: reference
order: 1
description: Every environment variable the four binaries read, and every file they write, per operating system.
---

# Environment variables and files

Defaults come from `awd help`, `aw help` and `aw-sync help`, and from the code that reads them.

## Install

On Linux or macOS, amd64 or arm64, this installs `aw`, `aw-policy`, `aw-sync` and `awd` to `~/.local/bin` without root:

```sh
curl -fsSL https://github.com/alchristleo/agentic-warden/releases/latest/download/install.sh | sh
```

`install.sh` needs `curl`, `tar`, and `sha256sum` or `shasum`. It downloads the archive for your system and `checksums.txt` from the same release, checks the archive against its line in `checksums.txt`, and only then copies the four binaries. It never uses `sudo`. It writes nothing to `AW_BIN_DIR` unless the archive verified and all four binaries were staged. Only the final four renames can fail midway, which can leave a mix of old and new binaries.

| Variable | Purpose |
|---|---|
| `AW_VERSION` | Release to install, such as `v0.1.0`. Default `latest`. |
| `AW_BIN_DIR` | Directory for the four binaries. Default `$HOME/.local/bin`. |
| `AW_BASE_URL` | Release base URL, for mirrors and tests. Default `https://github.com/alchristleo/agentic-warden/releases`. |

A pinned version downloads from `<base>/download/<version>/` instead of `<base>/latest/download/`:

```sh
curl -fsSL https://github.com/alchristleo/agentic-warden/releases/download/v0.1.0/install.sh | AW_VERSION=v0.1.0 sh
```

### Verify a download

Save the archive and `checksums.txt` from the same release in one directory, then run:

```sh
sha256sum --ignore-missing -c checksums.txt
```

This proves the archive matches the checksum published in its release. It catches corruption and tampering in transit. It does not prove who built the archive, because whoever can replace the release can replace its checksums too.

```sh
gh attestation verify agentic-warden_v0.1.0_linux_amd64.tar.gz -R alchristleo/agentic-warden
```

This proves GitHub recorded build provenance for the archive: a workflow run of this repository built it. It covers the case where the release itself is compromised. It needs the GitHub CLI. `install.sh` does not run it.

`install.sh` is not in `checksums.txt` and has no attestation, so read it before you run it.

### Install for enforcement

Enforcement needs a root-owned `aw-policy`, so install to a system directory with `sudo`:

```sh
curl -fsSL https://github.com/alchristleo/agentic-warden/releases/latest/download/install.sh | sudo env AW_BIN_DIR=/usr/local/bin sh
```

The script never calls `sudo` itself. Running it as root is your choice.

### From source

With Go 1.24 or newer:

```sh
go install github.com/alchristleo/agentic-warden/cmd/...@latest
```

This builds from the module source. It does not check a release checksum or attestation.

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
add `aw-bundle.json.sig` and `aw-trust.pub` in the state directory, beside the state copy of the bundle. `ProgramData` follows the `ProgramData` environment
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
