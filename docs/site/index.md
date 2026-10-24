---
title: Agentic Warden docs
section: get-started
order: 1
description: Write one policy, deliver it to Claude Code, Codex and Gemini CLI, and watch it stop your own agent. Seven steps, no root.
---

# Agentic Warden docs

See your own rule stop your own agent. Seven steps, no root. Then read what it enforces, where
it stops, and how to roll it out.

![aw claude refuses curl in payments-api and runs it in sandbox](../assets/hero.gif)

**[Start the quickstart](./quickstart.md)** · [Bring your rules](/demo)

Prerequisites: Linux or macOS, Go 1.24 or newer, and Claude Code installed and logged in. Docker is
not needed.

## Where to start

- **[Get started](./quickstart.md).** The 7-step quickstart, then
  [the same rule enforced on plain `claude`](./enforce-plain-claude.md) on a test VM.
- **[Security review](./security-model.md).** What it enforces, what it does not stop, who holds the
  keys, and [how to remove it](./removing.md).
- **[Write policy](./write-first-policy.md).** Rules per repository and per group, groups from Okta or
  Entra ID, and the self-hosted [admin console](./admin-console.md).

Working on the code? See [docs/contributing](https://github.com/alchristleo/agentic-warden/blob/main/docs/contributing/README.md).
