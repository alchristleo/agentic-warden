# agent-wrapper

Enterprise control plane for coding agents. An organization authors one policy;
each developer's session gets the slice of it that applies to them, targeted by
group and by repository.

Status: early. Claude Code is the only agent `aw` launches; `aw-sync` also
renders Codex's `requirements.toml` and Gemini's system settings and admin
policy.

## Why this exists

Claude Code already ships a lot of what an organization needs: managed settings
files, MDM delivery, server-managed settings from the claude.ai console,
`managedMcpServers`, sandboxing, and OpenTelemetry. Anthropic's self-hosted
Claude apps gateway adds SSO, per-IdP-group managed settings and spend limits
for organizations that bring their own API key or cloud provider.

What remains uncovered: **per-repository targeting**, per-group policy for
organizations on claude.ai seats (the console cannot target a group, and the
gateway needs an upstream credential), and any agent that is not Claude Code.
This project fills those gaps, on an agent-agnostic interface so other agents
can follow.

## How policy reaches the agent

Enforcement rides on `policyHelper`, a managed-only Claude Code setting naming
an executable that Claude Code runs at startup to compute the session's managed
settings. Its output becomes the top of the settings stack, above the command
line, the project and the user.

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
It exits non-zero only when the organization sets `requireBundle`. Every run
appends one line to a local audit log. `deploy/aw-sync/README.md` installs
the timer; `deploy/managed-settings/README.md` describes the files it writes.

Server-managed settings from the claude.ai console shadow the helper
entirely, so an organization uses one channel or the other; `aw doctor`
detects the collision, and reports the bundle and aw-sync's last cycle.

### Bundle signing

`awd keygen --out PATH` writes a 0600 Ed25519 seed and prints its public key
and key ID, and refuses to overwrite one that already exists.
`AWD_SIGNING_KEY` names that seed to `awd`, which then signs every `GET
/v1/bundle` response over the exact bytes it served: `X-AW-Signature` and
`X-AW-Key-Id` ride alongside a 200, and neither header rides a 304, because
there is no body and the machine keeps the bundle it already verified. A
rollover statement does ride a 304 — it is signed by the outgoing key and
says nothing about the body, so it stands on its own, and a fleet whose
policy is stable answers 304 to every cycle and would otherwise never
finish a rotation.
Leave the variable unset and bundles are unsigned — an existing deployment
keeps working exactly as it does today. A machine pins the key it saw at
enrollment into its own `machine.json` (root-owned, 0600, the same file
that already holds the machine credential), and every fetch after that is
verified against the pin before the body is parsed. Only a rollover
statement signed by the currently pinned key can move that pin, which is
what lets `AWD_SIGNING_KEY_PREVIOUS` carry a rotation through without
re-enrolling anyone. On a real machine `aw-sync` writes what it verified —
`aw-bundle.json` holding the served bytes verbatim, `aw-bundle.json.sig`,
and `aw-trust.pub`, the pinned key rendered where a developer's
`aw-policy` can read it — and `aw-policy` checks that trio before it
compiles anything: no trust file reads as an unsigned deployment and
today's behaviour, a bad signature reads as the `{}` envelope (or exit 1,
under `requireBundle`) rather than settings compiled from bytes nobody
vouched for. `aw`, `aw codex` and `aw gemini` apply the same check against
their own copy and refuse to launch on a bundle that fails it — for them
the bundle is the only input, and this refusal is `aw` being careful about
what it reads, not the enforcement boundary; that still lives in the
agent's own managed-settings tier, same as everywhere else in this section.
`aw doctor` reports which of the three states — verified, unsigned, or
failed — a machine is in, and `awd machines` shows the key ID each machine
last presented, which is how an operator watches a rotation finish. Doctor
also warns when the bundle or the trust file is writable by more than its
owner or owned by anyone but root, since an account that can rewrite both
can sign a policy of its own and have it verify. **On Windows it warns
that it cannot tell.** There is no uid behind the file there, and the ACL
that decides who may write it is not something `aw doctor` reads; the
finding says so rather than passing over the question in silence, because
a report that says nothing about ownership reads as one that looked and
found nothing wrong. On a Windows machine, check the ACLs of the state
directory by hand.

Signing proves the bytes on disk are the bytes `aw-sync` fetched; it does
not change who is able to write, chmod, or replace them. The trust anchor
and the bundle it checks share the same root-owned state directory, so
against genuine root this is **detection, not prevention**: root can
replace the binary doing the checking as easily as it can replace the
bundle sitting beside it. Where it does prevent rather than detect: a
`C:\ProgramData` subtree whose inherited ACLs let a standard user write
files the deploy assumed were administrator-only — the tampering is caught
there, though the loose ACL itself is not something doctor can report; a deploy that chmods or
chowns the state directory wrongly; a bundle copied between machines,
restored from a backup, or served by a stale cache; a compromised or
impersonated `aw-sync`. The fetch path is the one place this is strong
rather than advisory, because there the anchor is the 0600 `machine.json`,
not a file sitting next to what it verifies.

## Layout

    cmd/aw/           wrapper CLI: run an agent with the bundle compiled for its repository, doctor, agents
    cmd/aw-policy/    the policyHelper executable, offline
    cmd/aw-sync/      root-side sync: enroll, render every agent's files
    cmd/awd/          control plane: serve, apply
    deploy/           managed-settings install templates
    internal/agent/   Adapter interface, registry, Prepare/Launch/Exec
    internal/agent/claude/schema/  vendored settings schema and validation
    internal/agent/codex/  Codex adapter: requirements.toml renderer and key allowlist; -c overrides for aw codex
    internal/agent/gemini/  Gemini adapter: settings.json and admin policy renderer, key allowlist
    internal/policyhelper/  what aw-policy does: read the bundle, compile, validate, emit
    internal/sync/    the sync cycle: fetch the bundle, render, write all-or-nothing
    internal/policy/  rules, targeting, compilation, YAML/JSON authoring
    internal/repo/    which repository a directory is in, from .git/config
    internal/merge/   settings deep-merge and environment merge
    internal/decide/  tool-call decisions; the seam for a semantic decider
    internal/store/   policy revisions (in-memory and Postgres)
    internal/handler/ the HTTP layer
    internal/glob     wildcard matching for rules and repo targeting
    internal/cache/   content-addressed generated files
    internal/config/  control plane configuration
    internal/credential/  minting and hashing machine credentials

## Try it

With Docker, the whole control plane comes up on Postgres:

    docker compose -f deploy/docker-compose.yml up -d --build

It listens on :8080 with `AWD_ADMIN_TOKEN=change-me` unless you export
another. Without Docker, run it from source:

Run the control plane and apply a policy:

    go build -o awd ./cmd/awd
    AWD_ADDR=127.0.0.1:8080 ./awd serve &
    ./awd apply examples/org-policy.yaml --url http://127.0.0.1:8080

Apply needs an administrator token; set `AWD_ADMIN_TOKEN` for both the
server and the CLI. Then enroll a machine and fetch its bundle:

    export AWD_ADMIN_TOKEN=change-me
    TOKEN=$(./awd enroll-token alice@acme.com --url http://127.0.0.1:8080)
    curl -s -X POST http://127.0.0.1:8080/v1/machines/enroll \
      -d "{\"token\":\"$TOKEN\",\"name\":\"$(hostname)\",\"os\":\"linux\"}"
    # {"machineId":"...","credential":"...","user":"alice@acme.com"}
    curl -s -H 'Authorization: Bearer <credential>' http://127.0.0.1:8080/v1/bundle

Group membership has three sources. The policy's `groups` map is authored and
reviewed with the rules; it is the manual override. An identity-provider
snapshot is what the IdP says, posted whole by whatever export the operator
already trusts:

    awd groups apply examples/groups.yaml --url http://127.0.0.1:8080
    awd groups --url http://127.0.0.1:8080

A machine's bundle resolves its user's groups as the union of the three, so
a membership that must go away is removed from the source that added it.
User keys match the enrolled email exactly; there is no case folding. The
bundle's ETag covers the resolved rules, so a new snapshot reaches every
affected machine on its next `aw-sync` cycle. `awd groups` shows what the
server holds; the `GET /v1/groups` body is a report, not a re-appliable
file, because it carries fields the endpoint rejects on input.

The third is SCIM 2.0 provisioning. Set `AWD_SCIM_TOKEN` and point the
identity provider at `https://<awd>/scim/v2`:

- **Okta**: add a SCIM 2.0 app integration with *HTTP Header* authentication
  and the token as the bearer value; enable *Create Users*, *Update User
  Attributes*, *Deactivate Users* and *Push Groups*. Map `userName` to the
  email users enroll with.
- **Entra ID**: in an enterprise application, set provisioning to
  *Automatic*, the tenant URL to the SCIM root, and the secret token to
  `AWD_SCIM_TOKEN`. Map `userName` to whichever of `userPrincipalName` or
  `mail` equals the enrolled email.

SCIM groups union with the other two sources. Deactivating or deleting a
user in the IdP drops its SCIM groups on the next `aw-sync` cycle; it does
**not** revoke the user's machines — use `awd revoke` for that. Resolution
matches `userName` to the enrolled email exactly, and
`awd groups resolve <user> [--url URL]` shows each source's groups and warns when SCIM
holds the user under a different case.

The bundle is every rule that could apply to that user, with repository
matchers still in it; the machine resolves those per session. `aw-sync` does
the enrolling and the rendering on a real machine. For Codex it writes
`/etc/codex/requirements.toml` from the bundle's `codex` entries, and for
Gemini `/etc/gemini-cli/settings.json` and
`/etc/gemini-cli/policies/50-agent-wrapper.toml` from the `gemini` entries;
rules scoped to a repository cannot live in a static file and are reported by
`aw-sync status`. See `deploy/aw-sync/README.md` for installing it.

Fetch what a given developer would get:

    curl 'http://127.0.0.1:8080/v1/policy?group=platform&repo=github.com/acme/payments-api'

Run the sync and the policy helper the way a managed machine would, with the
files kept under the working directory so no root is needed:

    go build -o aw-sync ./cmd/aw-sync && go build -tags awtest -o aw-policy ./cmd/aw-policy
    TOKEN=$(./awd enroll-token alice@acme.com --url http://127.0.0.1:8080)
    AW_SYNC_TOKEN=$TOKEN ./aw-sync enroll --server http://127.0.0.1:8080 --agents claude --state-dir ./state
    ./aw-sync once --state-dir ./state --root claude=./claude-root
    AW_POLICY_BUNDLE=$PWD/claude-root/aw-bundle.json ./aw-policy

Stop `awd` and run `aw-policy` again: it needs no server. Run `aw-sync once`
again: it exits 1 and leaves the rendered files as they were.

The `-tags awtest` build is what lets `AW_POLICY_BUNDLE` point at the
working directory; it is for this walkthrough and the tests, never for a
machine you enrol.

Inspect what the wrapper would run, without running it:

    go build -o aw ./cmd/aw
    ./aw --policy <(curl -s http://127.0.0.1:8080/v1/policy) doctor

`doctor` prints the resolved binary, the exact arguments, the injected
environment and a note for every decision, plus aw-sync's last cycle
(`AW_SYNC_STATE_DIR=./state` points it at the directory above) and whether
a bundle is in place. It never executes the agent.

## Tests

    go test ./...

No test runs a real agent: fakes are executable files placed on a temporary
PATH. No test needs a network.

The Postgres store runs the same conformance suite as the in-memory one. It
needs a throwaway database and skips without one:

    AWD_TEST_DATABASE_URL=postgres://... go test ./internal/store/

## Security posture

This is a client-side control, not a security boundary. A developer with local
administrator rights can edit the managed source or run a modified client. Pair
it with scheduled MDM redeployment and network-level egress restriction. See
`docs/threat-model.md` when it lands.

## Admin console

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

## Marketing site

`web/` is the marketing site: a Next.js project with its own pnpm lockfile,
deployed to Vercel with `web/` as the project root. It does not share code
with the Go module.

    cd web
    pnpm install
    pnpm dev            # http://localhost:3000
    pnpm test           # unit tests
    pnpm test:e2e       # Playwright against a production build

Production deploys (`VERCEL_ENV=production`) need `NEXT_PUBLIC_SITE_URL`,
`RESEND_API_KEY`, `DEMO_INBOX` and `DEMO_FROM`; the build fails without
them. See `web/.env.example`.

## License

Apache License 2.0; see `LICENSE`.
