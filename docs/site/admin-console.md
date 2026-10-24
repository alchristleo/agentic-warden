---
title: Admin console
section: guides
order: 2
description: Turn on the self-hosted admin console with your OIDC identity provider. The hosted console is coming soon.
---

# Admin console

The hosted console is coming soon. Today you self-host `awd` and turn the console on yourself.

`awd` can serve a browser-based admin console at `/console/`, guarded by
your identity provider's OIDC sign-in rather than the bearer token. It is
opt-in: set `AWD_PUBLIC_URL` and every `AWD_CONSOLE_*` variable, or leave
them all unset and `/console/*` answers 404 with the token path unchanged.
Setting some but not all of them refuses to start `awd`, naming what is
missing.

Register `awd` as a confidential OIDC client with your identity provider
first:

- **Okta**: create an OIDC web app. Set the sign-in redirect URI to
  `https://<awd>/console/auth/callback` and the scopes to
  `openid email profile`.
- **Entra ID**: register a web platform app with the same redirect URI. Set
  `AWD_CONSOLE_USER_CLAIM=preferred_username`, since Entra ID's ID token
  does not carry `email` by default; if you would rather use `email`, add
  it as an optional ID token claim on the app registration instead.

| Variable | Purpose |
|---|---|
| `AWD_PUBLIC_URL` | External https origin of `awd` (e.g. `https://awd.example.com`); enables the console together with `AWD_CONSOLE_*`. Plain http is only accepted on `localhost`/`127.0.0.1`, for local development. |
| `AWD_CONSOLE_ISSUER` | The identity provider's OIDC issuer URL. |
| `AWD_CONSOLE_CLIENT_ID` | `awd`'s client id at the identity provider. |
| `AWD_CONSOLE_CLIENT_SECRET` | The OIDC client secret. Set this or `AWD_CONSOLE_CLIENT_SECRET_FILE`, not both. |
| `AWD_CONSOLE_CLIENT_SECRET_FILE` | Path to a file holding the OIDC client secret. |
| `AWD_CONSOLE_ADMIN_GROUP` | The identity-provider group whose members may administer the console. |
| `AWD_CONSOLE_USER_CLAIM` | ID token claim naming the signed-in user (default `email`). |

The console needs group data to know who is an admin: apply an
identity-provider snapshot with `awd groups apply`, enable SCIM, or both.
`AWD_CONSOLE_ADMIN_GROUP` must name a group that comes from the identity
provider (SCIM or the applied snapshot) — a group defined only in the
policy's authored `groups` map never grants console access, however it is
named.

`AWD_ADMIN_TOKEN` keeps working as the bearer-token recovery path
regardless of whether the console is enabled, so a broken IdP integration
never locks an operator out. Actions taken through the token path are
attributed in the audit log as `token:<AWD_APPLIED_BY>`, falling back to
`$USER` then `$USERNAME` when `AWD_APPLIED_BY` is unset, and just `token:`
when none of those are set, distinct from a console admin's own identity.

**Bring your rules.** See it against your own policy. [Request a demo](/demo)
