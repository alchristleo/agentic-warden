<p align="center"><img src="docs/assets/logo.svg" width="64" alt=""></p>
# Agentic Warden
**One policy for every coding agent.**

[![License](https://img.shields.io/github/license/alchristleo/agentic-warden)](https://github.com/alchristleo/agentic-warden/blob/main/LICENSE) [![web](https://github.com/alchristleo/agentic-warden/actions/workflows/web.yml/badge.svg)](https://github.com/alchristleo/agentic-warden/actions/workflows/web.yml)

Write your rules once. Agentic Warden delivers them to Claude Code, Codex and Gemini CLI on every
developer machine, scoped by identity-provider group and by repository. Claude Code applies them at
launch, and [on plain `claude`](docs/site/enforce-plain-claude.md) too once the managed files are installed.

![aw claude refuses curl in payments-api and runs it in sandbox](docs/assets/hero.gif)

Status: early. Self-host `awd` today. Hosted console: coming soon.

```yaml
rules:
  - name: baseline                  # no match block: applies everywhere
    agents: {claude: {managed: {permissions: {deny: ["Read(./.env)"]}}}}
  - name: payments-repos
    match: {repos: ["github.com/acme/payments*"]}
    agents: {claude: {managed: {permissions: {deny: ["Bash(curl *)"]}}}}
```

| As of October 2026           | Per repository | Per IdP group    | Claude Code, Codex, Gemini CLI |
|------------------------------|----------------|------------------|--------------------------------|
| Vendor admin console         | No             | Varies by vendor | One vendor each                |
| Managed settings files (MDM) | No             | No               | One format per vendor          |
| Agentic Warden               | Yes            | Yes              | One policy                     |

- **Works offline.** `aw` compiles from the last synced bundle on local disk, so a stopped control plane changes nothing. On bare `claude`, a missing or bad bundle fails open to the static files unless you set `requireBundle` ([security model](docs/site/security-model.md)).
- **Signed bundles.** When you configure a signing key, each machine enrolled after that verifies an Ed25519 or AWS KMS signature before it writes anything. Signing detects tampering and cannot stop a root user ([security model](docs/site/security-model.md)).
- **Out of the model path.** Your code, prompts and model traffic never reach the control plane ([trust statement](https://github.com/alchristleo/agentic-warden/blob/main/docs/hosting/trust.md)).

## Try it

The quickstart is seven steps, no root. These commands get you a running control plane with the policy applied:

```sh
git clone https://github.com/alchristleo/agentic-warden.git
cd agentic-warden
go build -o bin/ ./cmd/...
export PATH=$PWD/bin:$PATH
```

```sh
export AWD_ADDR=127.0.0.1:8080
export AWD_ADMIN_TOKEN=$(openssl rand -hex 16)
awd serve &
sleep 1
awd apply examples/quickstart.yaml
```

```text
applied revision quickstart.1 (2 rules)
```

The [full quickstart](docs/site/quickstart.md) enrolls your machine, then shows Claude refusing `curl` in one repository and running it in another.

[Quickstart](docs/site/quickstart.md) · [How it works](docs/site/how-it-works.md) · [Security model](docs/site/security-model.md) · [Admin console](docs/site/admin-console.md) · [Contributing](https://github.com/alchristleo/agentic-warden/blob/main/docs/contributing/README.md) · Apache-2.0
