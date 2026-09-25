# Admin console: a web UI over a self-hosted `awd`, with SSO

Date: 2026-09-25. Status: approved for planning. First half of milestone
(e), "hosted console". The milestone splits in two: this spec is the web
console every `awd` serves, self-hosted included; a later spec covers the
multi-tenant hosted `awd` and who holds each tenant's signing key. That
split lets the console ship to self-hosted customers now and leaves the
hosted trust model its own design rather than a corner of this one.

## Why this exists

Every admin action today goes through the `awd` CLI with one shared
`AWD_ADMIN_TOKEN`. Nobody is a person to the control plane: `applied_by`
is whatever the caller's environment says. An organization that wants
security staff to review policy, revoke a laptop or mint an enrollment
token must hand each of them the one secret that can do everything, and
afterwards cannot tell who did what.

The console gives admins a browser UI, signs them in through the company
IdP, and records every change against the person who made it.

## Decisions

- **Console served by `awd`.** A static SPA embedded with `go:embed` at
  `/console`, same origin as the API. Self-hosters still run one binary; no
  CORS; the future hosted `awd` serves the same bundle.
- **OIDC SSO, in `awd`.** Authorization code flow with PKCE; `awd` is a
  confidential client. Server-side sessions. `AWD_ADMIN_TOKEN` stays for
  the CLI and automation, unchanged.
- **Admin rights come from IdP-sourced group data `awd` already holds.** A
  user is a console admin when the SCIM groups or the applied snapshot put
  them in `AWD_CONSOLE_ADMIN_GROUP`. Authored groups (the policy's own
  `groups` map) never count: otherwise a policy revision could grant its
  author admin. The check runs on every request, so SCIM deprovisioning
  ends console access within one push, mid-session included.
- **No break-glass login.** With no group data, the console refuses (503)
  and the admin token remains the recovery path.
- **v1 scope is CLI parity minus group writes, plus an audit log.** Groups
  stay owned by SCIM and `awd groups apply`; the console never overwrites
  IdP data.
- **Every admin write is audited in the same transaction as the write.** A
  change that cannot be audited does not happen.

## Architecture

### Go packages

- `internal/console/oidc` — provider discovery, the login URL (state,
  nonce, PKCE S256), code exchange and ID token verification, over
  `github.com/coreos/go-oidc/v3` and `golang.org/x/oauth2`. Knows nothing of
  cookies or sessions. Discovery is lazy: attempted at startup, retried on
  the next login if it failed.
- `internal/console/session` — create, look up, touch and delete sessions.
  A session token is 32 random bytes, base64url; only its SHA-256 is
  stored. Lifetimes: 8 h absolute, 1 h idle. `last_seen` is written at most
  once a minute per session. Depends only on the store interface.
- `internal/console/authz` — `Admin(ctx, user) (bool, error)`: the union
  of `SCIMGroupsFor(user)` and the current snapshot's `Members[user]`
  contains `AWD_CONSOLE_ADMIN_GROUP`. Returns `ErrNoGroupData` when there
  are no SCIM groups at all and no snapshot. Matching is exact, as bundle
  resolution is.
- `internal/console/oidctest` — a fake IdP on `httptest`: discovery, JWKS,
  authorize and token endpoints, RS256-signed ID tokens, and switches to
  send a wrong nonce, wrong audience, expired token or
  `email_verified: false`. Also built as a small binary for the Playwright
  suite.
- `internal/handler/console.go` — the console routes, the `requireConsole`
  middleware and the static handler.

### Routes

| Route | Auth | Purpose |
|---|---|---|
| `GET /console/`, `/console/*` | none | SPA shell and assets; unknown paths under `/console/` that are not assets serve `index.html` |
| `GET /console/auth/login` | none | start OIDC login |
| `GET /console/auth/callback` | login cookie | finish login, create session |
| `POST /console/auth/logout` | session | delete session |
| `GET /console/api/me` | session | `{user, expiresAt}` or 401 |
| `GET /v1/audit?limit=&before=` | admin | audit events, newest first; `limit` default 50, max 200; `before` is an event id |

The existing admin routes change one thing: `requireAdmin` becomes
`requireAdminOrConsole`. A request with an `Authorization` header is judged
by the bearer token alone; a wrong token is 401 with no cookie fallback, so
a bad token is never masked. A request without one is judged by the
session cookie through `requireConsole`. Either way the middleware puts the
**actor** in the request context: the SSO user, or
`token:<X-Applied-By>` (`token:` alone when the header is empty) for the
bearer path. Handlers read `applied_by` and the audit actor from the
context, not from `X-Applied-By` directly. `/scim/v2` and machine routes
are untouched.

The static handler sets `Content-Security-Policy: default-src 'self';
script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:;
connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action
'self'`, `X-Content-Type-Options: nosniff` and `Referrer-Policy:
no-referrer`. Hashed assets are cached immutable; `index.html` is
`no-store`.

### Store

Migration `0006_console.sql`:

- `console_sessions (token_hash text primary key, user_name text not null,
  created_at timestamptz not null, last_seen_at timestamptz not null)`,
  index on `user_name`.
- `audit_events (id bigserial primary key, at timestamptz not null, actor
  text not null, action text not null, target text not null default '',
  detail jsonb not null default '{}')`, index on `at`.

Store interface additions: `CreateSession`, `SessionByHash`,
`TouchSession`, `DeleteSession`, `DeleteSessionsFor(user)`,
`DeleteExpiredSessions(before)`, and `AuditEvents(limit, before)`. Admin
writes take the audit event with them: `PutRuleSet`,
`PutEnrollmentToken`, `DeleteMachine` and `PutGroupSnapshot` gain an
`audit model.AuditEvent` argument, written in the same transaction
(Postgres) or under the same lock (memory). `RecordAudit` exists alone for
events with no other write (login, login denied, logout). Expired sessions
are swept hourly by a goroutine in `awd`.

Audited actions: `policy.revision.create` (target: version),
`enrollment_token.create` (target: user; never the token),
`machine.revoke` (target: machine id; detail: user, host),
`groups.snapshot.apply` (detail: user and group counts),
`console.login`, `console.login_denied` (detail: reason), `console.logout`.
Reads are not audited. SCIM writes are not audited in v1: the IdP keeps
its own provisioning log, and one row per SCIM PATCH would drown the admin
events.

### Configuration

| Env | Meaning |
|---|---|
| `AWD_PUBLIC_URL` | external base URL; redirect URI is `<url>/console/auth/callback`; also the expected `Origin` |
| `AWD_CONSOLE_ISSUER` | OIDC issuer URL |
| `AWD_CONSOLE_CLIENT_ID` | OIDC client id |
| `AWD_CONSOLE_CLIENT_SECRET_FILE` | file holding the client secret |
| `AWD_CONSOLE_ADMIN_GROUP` | group whose members are console admins |
| `AWD_CONSOLE_USER_CLAIM` | optional, default `email`; Entra ID setups use `preferred_username` so it matches the SCIM `userName` |

The five required variables are all set or all unset. All unset: `/console`
answers 404 and `awd` logs one info line. Some set: `awd` refuses to
start, naming the missing ones. `AWD_PUBLIC_URL` must be `https://`, except
`http://localhost` and `http://127.0.0.1` for development; cookies carry
`Secure` except on those.

## Data flow

**Login.**

1. `GET /console/auth/login` generates state, nonce and a PKCE verifier,
   stores them in an `aw_login` cookie (HttpOnly, `SameSite=Lax`, path
   `/console/auth`, 10 minutes) and redirects to the IdP. Lax, because the
   IdP's redirect back is a cross-site navigation that Strict would strip.
2. `GET /console/auth/callback` compares `state` to the cookie in constant
   time and clears the cookie, exchanges the code with the verifier, and
   verifies the ID token: issuer, audience, expiry, nonce. The user is the
   `AWD_CONSOLE_USER_CLAIM` claim. An `email_verified` claim that is
   present and false rejects the login.
3. `authz.Admin(user)`. On success: session row, `aw_session` cookie
   (HttpOnly, `SameSite=Strict`, path `/`, no `Max-Age` so it ends with the
   browser as well), audit `console.login`, redirect to `/console`. On
   failure: audit `console.login_denied`, 403 page.
4. The SPA shell needs no auth. It calls `/console/api/me`: a same-origin
   fetch, so the Strict cookie is sent. 401 shows the sign-in screen.

**Every console request.** Cookie → session by hash → absolute and idle
checks → touch → `authz.Admin` again → for non-GET requests, `Origin` must
equal `AWD_PUBLIC_URL` → handler with the actor in context.

**Logout.** Deletes the row, expires the cookie, audits `console.logout`.

## Failure modes

The plan copies every row into its Global Constraints; each row gets a
handler or integration test.

| Condition | Behavior |
|---|---|
| Console env partly set | `awd` refuses to start, naming the missing variables |
| Console env all unset | `/console/*` answers 404; token path unchanged |
| `AWD_PUBLIC_URL` plain http on a non-loopback host | `awd` refuses to start |
| IdP discovery fails at startup | `awd` starts; login answers 503 and retries discovery next attempt |
| Callback: state mismatch or login cookie missing | 400, no session |
| Callback: code exchange fails | 502, logged, no session |
| ID token invalid (signature, iss, aud, exp, nonce) | 401, warn log with reason, audit `console.login_denied` |
| `email_verified` present and false | 403, audit `console.login_denied` |
| User claim missing or empty | 403, audit `console.login_denied` |
| No group data (no SCIM groups, no snapshot) | 503 "console needs group data (SCIM or awd groups apply)" at login and on every request |
| User not in admin group at login | 403, audit `console.login_denied` |
| User leaves admin group or is deprovisioned mid-session | next request 403 and every session of that user deleted |
| Session past 8 h absolute or 1 h idle | 401, row deleted; SPA returns to sign-in |
| Non-GET console request with missing or foreign `Origin` | 403, no side effect |
| Wrong bearer token alongside a valid cookie | 401; the cookie is not consulted |
| Audit insert fails | the whole write fails with 500; no unaudited change |
| Store unavailable | 503 from session lookup; token path fails as it does today |
| Revision YAML does not parse (client) | editor shows the error at its line; nothing is sent |
| Revision rejected by server validation | 422 message shown above the editor; draft kept |

## Console (frontend)

`console/` at the repo root: Vite, React, TypeScript, shadcn base-nova with
the design tokens copied from `web/`, TanStack Query for requests,
TanStack Router for routes under `/console`. `vite build` writes to
`internal/console/dist`, which is committed so `go build` needs no Node;
CI rebuilds and fails on any difference.

Screens:

- **Overview** — current revision (version, author, time); machine count;
  machines pinning an old key ID; group data source and last sync; the
  five latest audit events.
- **Policy** — revision list; revision detail; line diff between any two
  revisions. "New revision" opens a CodeMirror 6 YAML editor, loaded
  lazily, prefilled with the current revision rendered as YAML. On submit
  the YAML is parsed in the browser (`yaml` package, duplicate keys
  rejected) and converted to JSON, as the CLI does; a confirm dialog shows
  the diff against the current revision before posting. Comments do not
  survive: revisions are stored as JSON. The editor says so.
- **Machines** — user, host, agents, last seen, bundle version, pinned key
  ID; stale machines and old keys flagged. Revoke asks the admin to type
  the hostname.
- **Enrollment** — create a token for a user with a TTL; the token and the
  `aw-sync enroll` command are shown once with a copy button and gone on
  navigation.
- **Groups** — source, last sync, groups with member counts, and a resolve
  box showing a user's groups by source. Read-only.
- **Audit** — paged table, filter by actor and action (client-side over
  the fetched page in v1).

Every screen: user and logout in the header; 401 anywhere goes to sign-in;
403 shows "You are not a console admin"; light and dark themes.

## Testing

- **Go units.** `session`: token hashing, absolute and idle expiry at the
  boundaries, touch throttling. `authz`: SCIM only, snapshot only, both,
  authored groups ignored, no data. `oidc`: every `oidctest` switch.
- **Handler tests.** One per failure-mode row, plus: the bearer path
  records `token:<X-Applied-By>` as actor; the console path records the SSO
  user; each audited action writes exactly one event with the documented
  target.
- **Store conformance** (`storetest`). Sessions and audit, including that a
  failing audit insert leaves the paired write undone. Runs on memory and on
  Postgres in Docker.
- **Frontend.** Vitest, Testing Library and MSW per screen; axe on each.
- **End to end.** Playwright against a real `awd` (memory store) and the
  `oidctest` binary: sign in, create a revision, revoke a machine, see both
  in Audit; a non-admin is refused.
- **CI.** New `.github/workflows/console.yml`: install, test, build, and
  `git diff --exit-code internal/console/dist`; Playwright job.

## Documentation

README section "Admin console": IdP app registration for Okta and Entra ID
(redirect URI, scopes `openid email profile`, which claim to pick), the env
table, the group-data prerequisite, and that the admin token remains the
recovery path. Marketing copy about the console stays "coming soon" until
this ships and then describes only what it does.

## Out of scope, deliberately

- Multi-tenant hosted `awd` and signing-key custody for tenants: the next
  spec.
- Editing groups from the console.
- Roles narrower than admin (read-only viewers, approvers).
- SAML, local accounts, a break-glass login.
- Server-side audit filtering and export.
