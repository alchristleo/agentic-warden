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
| `AWD_ADMIN_TOKEN` | Bearer token for `apply` and machine administration, with the audit name `admin`. |
| `AWD_ADMIN_TOKENS_FILE` | File with one `<name> <sha256-hex>` line per admin. Blank lines and `#` comments are allowed. A bad line stops startup and the error names the line number. Create lines with `awd admin-token <name>`. With neither this nor `AWD_ADMIN_TOKEN` set, the admin routes answer 503. |
| `AWD_TLS_CERT_FILE`, `AWD_TLS_KEY_FILE` | PEM certificate chain and private key. Set both or neither. Both set: `awd` serves HTTPS, TLS 1.2 and up. Restart `awd` to load a new certificate. |
| `AWD_TRUSTED_PROXIES` | Comma-separated CIDRs of your proxies or load balancer. The failed-auth limiter reads `X-Forwarded-For` only from these addresses. |
| `AWD_SCIM_TOKEN` | Bearer token the identity provider presents on `/scim/v2`. Unset disables SCIM. |
| `AWD_SIGNING_KEY` | Path to a signing key written by `awd keygen`, or `awskms:<key ARN>`. Unset means bundles are not signed. |
| `AWD_SIGNING_KEY_PREVIOUS` | The key being rotated out, signed over during a rotation. |
| `AWD_PUBLIC_URL`, `AWD_CONSOLE_*` | Turn on the admin console. All or none; see [Admin console](./admin-console.md). |

### Admin tokens, TLS and the failed-auth limiter

Mint a token for each admin. `awd admin-token` reads no configuration and writes no file. It prints two lines.

```sh
awd admin-token alice
```

```text
VWNV5V7Lget8lXyM2G_2bdBbyWbnJWzQ8DqtmeCvSCQ
alice 6b7fcadac4bb0f2dca1285732b04c2639a5ef14123b9466e2df656bc873aee92
```

Line 1 is the token. Give it to Alice and keep it out of the file. Line 2 goes into the tokens file. Do not redirect the whole output into the file, because line 1 is the token. This appends only line 2 and prints only the token:

```sh
out=$(awd admin-token alice)
printf '%s\n' "$out" | tail -n 1 >> /etc/awd/admin-tokens
printf '%s\n' "$out" | head -n 1
```

Set `AWD_ADMIN_TOKENS_FILE=/etc/awd/admin-tokens` and restart `awd`. To revoke an admin, delete the line and restart. A name has 1 to 64 characters from `a-z`, `0-9`, `.`, `_`, `@` and `-`. It starts with a letter or digit. A file entry named `admin` next to `AWD_ADMIN_TOKEN` stops startup. The audit actor is `token:<name>`. The tokens file holds hashes only (`TestAdminTokenCommand` in `cmd/awd/wiring_test.go`, `internal/config/admintokens_test.go`).

For a self-signed certificate on Linux, run `SSL_CERT_FILE=/path/to/awd.pem aw-sync run` and the `awd` subcommands the same way. `aw-sync` and `awd` have no other option for a custom CA.

The limiter counts failed attempts per client IP: 401 on the admin routes, the console and SCIM, and 404 or 409 on enrollment. It does not count 401 on `GET /v1/bundle`. The 11th failure in a row gets `429` with a `Retry-After` header and the body `{"error":"too many failed attempts"}`. After that, one attempt is allowed every 6 seconds. `/healthz` and `/readyz` are exempt. IPv6 clients share a bucket per /64. When `AWD_TRUSTED_PROXIES` is set, a request from a listed address is counted against the rightmost `X-Forwarded-For` entry that is not itself listed. Clients behind one NAT share a bucket. If `awd` sees `X-Forwarded-For` with `AWD_TRUSTED_PROXIES` unset, it logs one warning. Evidence: `internal/handler/limit_test.go` (`TestLimiterBlocksAfterTenFailures`, `TestLimiterRefills`, `TestLimiterCountsEnroll404And409`, `TestLimiterSkipsBundle401`, `TestLimiterKeyTrustedRightmostUntrusted`, `TestLimiterKeyIPv6SharesSlash64`, `TestLimiterKeyWarnsOnceWithoutTrustedProxies`).

## Command-line clients


| Variable | Read by | Purpose |
|---|---|---|
| `AWD_URL` | `awd` subcommands | Control plane URL used by `apply` and the admin commands. Default `http://localhost:8080`. |
| `AWD_ADMIN_TOKEN` | `awd` subcommands | One of the tokens the server accepts. |
| `SSL_CERT_FILE` | `aw-sync`, `awd` subcommands | On Linux, a PEM file of extra trusted certificates. Point it at a self-signed `awd` certificate. This is Go's standard variable. |
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
| agy `admin_settings.json`, opt-in | `/etc/antigravity` | `/Library/Application Support/Antigravity` | `%ProgramData%\Antigravity` |
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

## Gemini CLI (removed)

agent-wrapper no longer supports Gemini CLI. Google retired Gemini CLI for most users on 2026-06-18 ([announcement coverage](https://www.theregister.com/ai-ml/2026/05/20/bye-bye-gemini-cli-google-nudges-devs-toward-antigravity/5243605)). Use Antigravity CLI (`agy`) instead. aw governs only its gateway and models ([how it works](./how-it-works.md#antigravity-cli-gateway-and-models-only)).

If you still run Gemini CLI under a paid licence, aw no longer governs it. Keep an older agent-wrapper release or move to agy.

Existing rules:

- `awd apply` rejects a rule that still has a `gemini` block. The error names the rule and the block to delete:

  ```text
  rule "payments-repos": agent "gemini" was removed: delete the gemini block from this rule;
  Gemini CLI is no longer supported by agent-wrapper;
  use agy (Antigravity CLI) (docs/site/reference.md#gemini-cli-removed)
  ```

  Delete the block, then apply again. To keep the parts that agy supports, move them to an `agy` block.
- A policy stored before the removal keeps working. aw ignores its `gemini` block.

Enrollments:

- `aw-sync enroll --agents gemini` fails with the same hint.
- An enrolled machine skips `gemini`, prints a note, and syncs the other agents.
- aw-sync removes the Gemini files it wrote if you have not changed them. It keeps a changed file and prints a note. [Removing](./removing.md) lists the paths.
- To stop syncing Gemini, re-enroll with `--force`, `--server`, a new enrollment token, and an `--agents` list that leaves out `gemini`, such as `--agents claude,codex`.

`aw gemini` exits with an error that tells you to use `aw agy`.

## Files written by `aw`

`aw` writes the merged settings it hands to the agent under the user cache directory:
`~/.cache/agent-wrapper/<agent>/settings-<hash>.json` on Linux, and the platform's equivalent
elsewhere.

## agy managed keys

`agents.agy.managed` is the content of agy's `admin_settings.json`. `awd apply` and `aw-sync` check it
against these rules, and an unknown key at any level is an error:

| Key | Rule |
|---|---|
| `gateway.enabled` | A boolean. `true` needs `gateway.url`. |
| `gateway.url` | An `https://` address. |
| `gateway.wireProtocol` | `genai` or `openai`. |
| `gateway.customHeaders` | Header names that contain `auth`, `key`, `token`, `secret`, `cookie` or `password` (any case) are errors. The check is a substring match, so it is best effort. |
| `gateway.apiKey` | Always an error. Set `AGY_LLM_GATEWAY_API_KEY` on each developer machine instead. |
| `caCertPath` | An absolute path. |
| `models.defaultModelId` | A non-empty string. When you set `models.list`, the default must be in the list. |
| `models.list[].modelId` | A non-empty string. |

## Files written by the rootless trial

With `--state-dir ~/.aw-try` and `--root claude=~/.aw-try/claude`, everything above moves under
`~/.aw-try`, except the `aw` cache. [Removing Agentic Warden](./removing.md) lists them.
