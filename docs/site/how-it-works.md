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
against a developer's own settings is Claude Code's behavior (the recorded run in
quickstart step 6, through `aw claude` and `--settings`, shows an organization deny
beating a command-line allow; the repository has no recorded run of bare `claude`
with the helper).

That matters because the helper runs on `claude`, not on `aw claude`. A
developer who bypasses the wrapper is still governed.

`aw codex` applies the rules scoped to the repository you are
in, which the machine-wide files cannot carry. `aw` reads the bundle
`aw-sync` leaves in its state directory, compiles it for the repository
`.git/config` names, and hands each agent the result through its own
launch-time channel: Codex gets `-c key=value` overrides from the rule's
`launch` document (its `config.toml` schema; `managed` stays
`requirements.toml`). Because Codex receives them as `-c` overrides, every
key in a launch document — including an MCP server name — must use only
letters, digits, `_` and `-`; `awd apply` refuses a rule with any other
key. This layer is advisory: bare `codex` sees the machine-wide files alone. `aw doctor` shows the compiled result per agent and which repository it was compiled for.

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
Once a key is pinned it exits non-zero instead, unless the organization sets
`requireBundle` to `false`; `requireBundle: true` does it on every machine. Each run
tries to append one line to a local audit log; the write is best effort and is skipped silently if
it fails. [deploy/aw-sync/README.md](https://github.com/alchristleo/agentic-warden/blob/main/deploy/aw-sync/README.md)
installs the timer;
[deploy/managed-settings/README.md](https://github.com/alchristleo/agentic-warden/blob/main/deploy/managed-settings/README.md)
describes the files it writes.

Server-managed settings from the claude.ai console shadow the helper
entirely, so an organization uses one channel or the other; `aw doctor`
detects the collision, and reports the bundle and aw-sync's last cycle.

## Antigravity CLI (gateway and models only)

Google retired Gemini CLI for most users on 2026-06-18 ([announcement coverage](https://www.theregister.com/ai-ml/2026/05/20/bye-bye-gemini-cli-google-nudges-devs-toward-antigravity/5243605)); Gemini Code Assist Standard and Enterprise licences and paid API keys keep it. Its successor is Antigravity CLI (`agy`).
`aw-sync` renders agy only when you enroll with `--agents ...,agy`. Then it writes agy's
machine-wide admin file, and it owns that whole file. agy reads the gateway, CA bundle and
model list from that file, and a developer cannot override them. agy has no path override, so
`aw agy` cannot scope the file to a repository. aw drops repository-scoped agy rules and prints
a note.

Not enforced:

- agy permissions
- MCP servers
- hooks
- repository-scoped rules

agy's permission rules, MCP servers and hooks live only in each developer's home directory.

The gateway API key stays out of the policy. aw does not distribute this key. Issue it per developer at your
gateway and set it through your usual secret or shell-profile channel, as `AGY_LLM_GATEWAY_API_KEY`.
`aw doctor agy` reports whether the admin file is present. It says "admin file present, revision X",
not "enforced", because agy may ignore the file. It warns when the file does not pass aw's policy checks
(agy may still accept it). It reports an error only when the file is unreadable or not valid JSON,
because agy silently ignores a file it cannot parse.

## Compared with what you have today

As of October 2026. This compares mechanisms only.

| As of October 2026            | Per repository | Per IdP group      | Claude Code, Codex, Antigravity CLI |
|-------------------------------|----------------|--------------------|-------------------------------------------------|
| Vendor admin console          | No             | Varies by vendor   | One vendor each                |
| Managed settings files (MDM)  | No             | No                 | One format per vendor          |
| Agentic Warden                | Yes            | Yes                | One policy (Antigravity: gateway and models only) |

Claude Code already ships managed settings files, MDM delivery, server-managed settings from the
claude.ai console, `managedMcpServers`, sandboxing, and OpenTelemetry. Anthropic's self-hosted Claude
apps gateway adds SSO, per-IdP-group managed settings and spend limits for organizations that bring
their own API key or cloud provider. What remains uncovered:

- Per-repository rules.
- Per-group policy for Claude Code on claude.ai seats (the console is organization-uniform; the
  gateway needs an upstream API or cloud credential).
- One policy across Claude Code, Codex and Antigravity CLI (gateway and models only).

Codex on ChatGPT plans already has per-group rules in OpenAI's Agent Security, so for Codex this
project adds per-repository rules, API-key use and self-hosting.

## Signing and its limits

Bundles can be signed with an Ed25519 key or an AWS KMS key. Signing detects tampering; it does not
stop a root user. See the [security model](./security-model.md).
