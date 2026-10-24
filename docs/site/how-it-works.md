---
title: How it works
section: get-started
order: 4
description: How a policy gets from the control plane to an agent, why it still applies offline, and how it differs from vendor consoles and MDM files.
---

# How it works

Enforcement rides on `policyHelper`, a managed-only Claude Code setting naming
an executable that Claude Code runs at startup to compute the session's managed
settings. Claude Code applies its output as managed settings; how those rank
against a developer's own settings is Claude Code's behavior (quickstart step 6
records an organization deny beating a command-line allow).

That matters because the helper runs on `claude`, not on `aw claude`. A
developer who bypasses the wrapper is still governed.

`aw codex` and `aw gemini` apply the rules scoped to the repository you are
in, which the machine-wide files cannot carry. `aw` reads the bundle
`aw-sync` leaves in its state directory, compiles it for the repository
`.git/config` names, and hands each agent the result through its own
launch-time channel: Codex gets `-c key=value` overrides from the rule's
`launch` document (its `config.toml` schema; `managed` stays
`requirements.toml`). Because Codex receives them as `-c` overrides, every
key in a launch document — including an MCP server name — must use only
letters, digits, `_` and `-`; `awd apply` refuses a rule with any other
key. Gemini gets a settings file generated for the
session with `GEMINI_CLI_SYSTEM_SETTINGS_PATH` pinned to it, repo-scoped
`policies` included via `policyPaths`. This layer is advisory: bare
`codex` or `gemini` sees the machine-wide files alone. With no policy
source, `aw gemini` leaves the system settings path alone rather than
pinning an empty file over it. `aw doctor` shows the compiled result per
agent and which repository it was compiled for.

One Gemini limitation: rules delivered through `policyPaths` load at
Gemini's user tier, below the admin tier where `aw-sync`'s machine-wide
policy file lives, and Gemini ignores admin-tier supplements once that
directory has any file. A machine-wide rule that names a tool therefore
outranks a repo-scoped rule for the same tool. Keep machine-wide
`policies` to what must hold everywhere and let repo rules tighten the
rest.

A release build of the helper reads only the system directory. The
`AW_POLICY_BUNDLE` and `AW_POLICY_CONFIG` overrides the tests use exist
only under `-tags awtest`; a release build ignores them and says so on
stderr, and `aw doctor` warns if a tagged helper is installed. What remains
outside the helper's control is which repository a session is in:
repo-scoped rules key on `.git/config`'s origin, which is the developer's
to edit.

Install `aw-sync` and `aw-policy` once; `aw-sync` writes the drop-in that
names the helper, and every change after that is a server-side decision.

**A helper that exits non-zero, or emits settings that fail schema validation,
makes Claude Code refuse to start.** The helper must therefore always exit 0.
That contract is the highest-severity path in the project.

`aw-policy` is that helper, and it is offline. `aw-sync`, running as root on
a timer, fetches the enrolled user's bundle from the control plane and writes
it to Claude Code's system directory as `aw-bundle.json`, beside a drop-in
that names the helper. On every launch `aw-policy` reads that bundle, makes
the one decision left in it (which repository the session is in), validates
the result against the published Claude Code settings schema, and prints it.
With no bundle, or one it cannot use, it prints an envelope with no settings,
which leaves the static managed-settings files in force, and still exits 0.
It exits non-zero only when the organization sets `requireBundle`. Each run
tries to append one line to a local audit log; the write is best effort and is skipped silently if
it fails. [deploy/aw-sync/README.md](https://github.com/alchristleo/agentic-warden/blob/main/deploy/aw-sync/README.md)
installs the timer;
[deploy/managed-settings/README.md](https://github.com/alchristleo/agentic-warden/blob/main/deploy/managed-settings/README.md)
describes the files it writes.

Server-managed settings from the claude.ai console shadow the helper
entirely, so an organization uses one channel or the other; `aw doctor`
detects the collision, and reports the bundle and aw-sync's last cycle.

## Compared with what you have today

As of October 2026. This compares mechanisms only.

| As of October 2026            | Per repository | Per IdP group      | Claude Code, Codex, Gemini CLI |
|-------------------------------|----------------|--------------------|--------------------------------|
| Vendor admin console          | No             | Varies by vendor   | One vendor each                |
| Managed settings files (MDM)  | No             | No                 | One format per vendor          |
| Agentic Warden                | Yes            | Yes                | One policy                     |

Claude Code already ships managed settings files, MDM delivery, server-managed settings from the
claude.ai console, `managedMcpServers`, sandboxing, and OpenTelemetry. Anthropic's self-hosted Claude
apps gateway adds SSO, per-IdP-group managed settings and spend limits for organizations that bring
their own API key or cloud provider. What remains uncovered:

- Per-repository rules.
- Per-group policy for Claude Code on claude.ai seats (the console is organization-uniform; the
  gateway needs an upstream API or cloud credential).
- One policy across Claude Code, Codex and Gemini CLI.

Codex on ChatGPT plans already has per-group rules in OpenAI's Agent Security, so for Codex this
project adds per-repository rules, API-key use and self-hosting.

## Signing and its limits

Bundles can be signed with an Ed25519 key or an AWS KMS key. Signing detects tampering; it does not
stop a root user. See the [security model](./security-model.md).
