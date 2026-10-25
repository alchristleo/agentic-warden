---
title: Install, environment variables and files
section: reference
order: 1
description: How to install and verify the release, every environment variable the four binaries read, and every file they write, per operating system.
---

# Install, environment variables and files

## Install

On Linux or macOS, amd64 or arm64, this installs `aw`, `aw-policy`, `aw-sync` and `awd` to `~/.local/bin` without root:

```sh
REL=https://github.com/alchristleo/agentic-warden/releases
curl -fsSL $REL/latest/download/install.sh \
  | sh
```

Check: `~/.local/bin/aw version`. If `aw` alone is not found, add `export PATH="$HOME/.local/bin:$PATH"` to your shell profile. Next: [Quickstart](./quickstart.md).

`install.sh` needs `curl`, `tar`, `sha256sum` or `shasum`, and standard POSIX tools. It downloads the archive for your system and `checksums.txt` from the same release, checks the archive against its line in `checksums.txt`, and only then copies the four binaries. It never uses `sudo`.

It changes nothing in `AW_BIN_DIR` (beyond creating it) unless the archive verified and all four binaries were staged. Only the final four renames can fail midway, which can leave a mix of old and new binaries.

Two variables change what it does:

- `AW_VERSION`: the release to install, with its leading `v`, such as `v0.1.1`. Default `latest`.
- `AW_BIN_DIR`: where the four binaries go. Default `$HOME/.local/bin`.

`AW_BASE_URL` overrides the release base URL (used by the installer tests).

A pinned version downloads from `<base>/download/<version>/` instead of `<base>/latest/download/`. Replace `<version>` with yours, keeping the leading `v` (`v0.1.1`):

```sh
REL=https://github.com/alchristleo/agentic-warden/releases
curl -fsSL $REL/download/<version>/install.sh \
  | AW_VERSION=<version> sh
```

### Verify a download

Download the archive (`agentic-warden_<version>_<os>_<arch>.tar.gz`) and `checksums.txt` from the same release on the [Releases page](https://github.com/alchristleo/agentic-warden/releases) into one directory, then run:

```sh
sha256sum --ignore-missing -c checksums.txt   # Linux
shasum -a 256 --ignore-missing -c checksums.txt   # macOS
```

This proves the archive matches the checksum published in its release. It catches a corrupted download. It does not prove who built the archive, because whoever can replace the release can replace its checksums too.

```sh
gh attestation verify \
  agentic-warden_<version>_<os>_<arch>.tar.gz \
  -R alchristleo/agentic-warden
```

Replace the version, OS and architecture with yours. This proves GitHub recorded build provenance for the archive: a workflow run of this repository built it. It covers the case where the release itself is compromised. It needs the GitHub CLI. `install.sh` does not run it.

The release workflow attests `install.sh` and `checksums.txt` as well, so `gh attestation verify install.sh -R alchristleo/agentic-warden` works on a downloaded copy. `install.sh` is still not listed in `checksums.txt`, so read it before you run it.

To install an archive you verified yourself:

```sh
tar -xzf <archive>
mkdir -p ~/.local/bin
install -m 0755 aw aw-policy aw-sync awd \
  ~/.local/bin/
```

### Install for enforcement

Enforcement needs a root-owned `aw-policy`, so install to a system directory with `sudo`. Download the script, read it, then run it:

```sh
REL=https://github.com/alchristleo/agentic-warden/releases
curl -fsSLO $REL/latest/download/install.sh
less install.sh
sudo env AW_BIN_DIR=/usr/local/bin sh install.sh
```

Press `q` to leave `less`. Check: `/usr/local/bin/aw version`.

The script never calls `sudo` itself. Running it as root is your choice. The directory must be root-owned and not writable by your user. Check with `ls -ld /usr/local/bin`: the third column is the owner, and it should be `root`. If your user owns it, use another root-owned directory.

### From source

With Go 1.24 or newer:

```sh
go install \
  github.com/alchristleo/agentic-warden/cmd/...@latest
```

This builds from the module source. It does not check a release checksum or attestation. The binaries land in `$(go env GOPATH)/bin`, which is often not on your `PATH`. The enforcement guidance above (a root-owned `aw-policy`) does not apply to this path.

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
