---
title: Write your first policy
section: guides
order: 1
description: The shape of a policy file, how rules target repositories and groups, how rules combine, and how groups reach the control plane.
---

# Write your first policy

A policy is one YAML file with a version, an optional `groups` map and a list of rules. You apply it
with `awd apply`, and every enrolled machine picks it up on its next `aw-sync` cycle. The
[quickstart](./quickstart.md) uses the smallest useful one,
[examples/quickstart.yaml](https://github.com/alchristleo/agentic-warden/blob/main/examples/quickstart.yaml).
A fuller one is
[examples/org-policy.yaml](https://github.com/alchristleo/agentic-warden/blob/main/examples/org-policy.yaml).

## A rule

```yaml
version: "2026-10-24.1"

rules:
  - name: baseline
    agents:
      claude:
        managed:
          permissions:
            deny:
              - Read(./.env)
```

- `name` identifies the rule.
- `match` narrows who the rule applies to. A rule with no `match` block applies to everyone.
- `agents` holds one block per agent: `claude`, `codex`, `gemini` or `agy`.
- `managed` is the machine-wide layer. For Claude it is the managed settings; for Codex it is
  `requirements.toml`; for Gemini it is the system settings and the admin policy; for agy it is the
  admin settings file.
- `launch` is Codex's per-launch `config.toml` layer, applied by `aw codex` and not by `aw-sync`.
- `env` and `forceEnv` set environment variables. With `forceEnv: true`, organization values replace
  what a developer already exports.

`awd apply` refuses a rule it cannot use, for example a Codex
`launch` key with a character other than letters, digits, `_` and `-`.

## Rules for one repository

```yaml
  - name: payments-repos
    match:
      repos: ["github.com/acme/payments*"]
    agents:
      claude:
        managed:
          permissions:
            deny:
              - Bash(curl *)
```

Repository patterns are globs, and `*` crosses path separators. The machine resolves them per
session from the origin remote in `.git/config`. That origin is the developer's to edit, which is a
limit worth knowing: see the [security model](./security-model.md).

## Rules for a group

```yaml
groups:
  alice@acme.com: [platform]
  bob@acme.com: [platform, security]

rules:
  - name: platform-team
    match:
      groups: [platform]
    agents:
      claude:
        managed:
          model: opus
```

Group names compare exactly. User keys match the enrolled email exactly, with no case folding.

## Rules for agy

```yaml
rules:
  - name: agy-gateway
    agents:
      agy:
        managed:
          gateway:
            enabled: true
            url: https://gateway.example.com
            wireProtocol: genai
            customHeaders:
              X-Cost-Center: platform
          caCertPath: /etc/ssl/certs/example-ca.pem
          models:
            defaultModelId: gemini-3.8-flash
            list:
              - modelId: gemini-3.8-flash
              - modelId: gemini-3.8-pro
```

The model IDs are examples. Use the models your gateway serves, which `agy models` lists.

`managed` for agy is the admin settings file, so it applies to every developer on the machine.
`awd apply` refuses a `launch` block for agy. Set `AGY_LLM_GATEWAY_API_KEY` on each developer machine, not in
`env`. [How it works](./how-it-works.md) lists what agy does not enforce.

## How rules combine

Rules apply in order, and a later rule overrides an earlier one per key. Permission lists union
across rules instead of replacing one another. Codex scalars and allowlists replace across rules, and
`prefix_rules` accumulate. For Gemini, `tools.exclude` and `mcp.allowed` union, `mcpServers` merge by
name, other settings replace, and policies accumulate. Every Gemini policy rule needs `toolName`,
`decision` and an integer `priority` from 0 to 999. For agy, `models.list` replaces across rules, and
`gateway.customHeaders` accumulate, with a later rule winning per header name.

`aw doctor` shows the compiled result for the repository you are in.

## Where group membership comes from

Group membership has three sources. A machine's bundle resolves its user's groups as the union of all
three, so a membership that must go away is removed from the source that added it.

1. The policy's `groups` map. It is authored and reviewed with the rules, and is the manual override.
2. An identity-provider snapshot, posted whole by whatever export you already trust:

       awd groups apply examples/groups.yaml
       awd groups

   `awd groups` shows what the server holds. A new snapshot reaches every affected machine on its next
   `aw-sync` cycle.
3. SCIM 2.0 provisioning. Set `AWD_SCIM_TOKEN` and point your identity provider at
   `https://<awd>/scim/v2`.

   - **Okta**: add a SCIM 2.0 app integration with *HTTP Header* authentication and the token as the
     bearer value. Enable *Create Users*, *Update User Attributes*, *Deactivate Users* and *Push
     Groups*. Map `userName` to the email users enroll with.
   - **Entra ID**: in an enterprise application, set provisioning to *Automatic*, the tenant URL to the
     SCIM root, and the secret token to `AWD_SCIM_TOKEN`. Map `userName` to whichever of
     `userPrincipalName` or `mail` equals the enrolled email.

SCIM groups union with the other two sources. Deactivating or deleting a user in the identity provider
drops its SCIM groups on the next `aw-sync` cycle. It does not revoke the user's machines; use
`awd revoke` for that. `awd groups resolve <user>` shows each source's groups and warns when SCIM
holds the user under a different case.

## Apply and check

    awd apply my-policy.yaml
    aw-sync once          # on a machine, or wait for the timer
    aw doctor             # in a repository
