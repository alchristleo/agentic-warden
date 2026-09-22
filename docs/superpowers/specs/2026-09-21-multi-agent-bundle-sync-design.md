# Multi-agent governance: bundles, machine enrollment, aw-sync

Date: 2026-09-21. Status: implemented through M4e on 2026-09-22; the "Out of scope" section is what remains.
Supersedes milestones 4-7 of the original plan.

## Why this exists

Anthropic's self-hosted Claude apps gateway now covers SSO, per-IdP-group
managed settings, spend limits and telemetry for organizations that bring an
API key or cloud provider. Identity brokering and budgets are no longer a
reason for this project to exist. Two things are:

- **One policy for every agent.** Claude Code, Codex CLI and Gemini CLI each
  have an admin-enforced settings tier, and none of the three vendors will
  govern the other two. Security teams have all three in the fleet.
- **Per-repository targeting**, which no vendor offers, and which is natively
  enforceable only on Claude Code through `policyHelper`.

Enforcement seams, verified against each vendor's documentation on 2026-09-21:

| | Claude Code | Codex CLI | Gemini CLI |
| --- | --- | --- | --- |
| Enforced tier | `managed-settings.json`, MDM, server-managed | `/etc/codex/requirements.toml`, `%ProgramData%\OpenAI\Codex\requirements.toml`, MDM `com.openai.codex:requirements_toml_base64`, cloud (ChatGPT plans) | `/etc/gemini-cli/settings.json` and `policies/*.toml` (admin tier); macOS `/Library/Application Support/GeminiCli/`; Windows `C:\ProgramData\gemini-cli\` |
| Per-launch dynamic source | `policyHelper` | none | none |
| Native per-group | gateway (API orgs) | cloud requirements per identity (ChatGPT Business/Enterprise) | none |
| Native per-repo | none | none | none |
| Format | JSON | TOML | JSON settings, TOML policy rules |

Consequence: Codex and Gemini are governed by static root-owned files. Something
on each machine has to write them per group. That is `aw-sync`. Claude Code's
helper keeps per-repo targeting because it runs per launch in the session's
directory; it now reads a local bundle instead of the network.

## Architecture

```
 policy.yaml ──awd apply──▶ awd ──GET /v1/bundle (machine credential)──▶ aw-sync (root, timer)
                                                                            │ renders, atomically
                                ┌───────────────────────────────────────────┼───────────────────┐
                                ▼                                           ▼                   ▼
                 /etc/claude-code/aw-bundle.json            /etc/codex/requirements.toml   /etc/gemini-cli/settings.json
                                │                                                          /etc/gemini-cli/policies/50-agent-wrapper.toml
                     aw-policy (per launch, offline)
                     repo from cwd → compile → managedSettings
```

One network client per machine (`aw-sync`), one credential (the machine's).
`aw-policy` touches only local files: the bundle, `.git/config`, its optional
configuration and its audit log.

A machine is enrolled for one user. Shared hosts and CI runners enroll with
a service user whose groups express what that host may do; per-developer
policy on a shared host is out of scope for v1.

## Control plane

### Data model (`internal/model`)

- `Machine{ID, User, Name, OS, CredentialHash, EnrolledAt, LastSeenAt, LastBundleVersion}`.
  The credential is 32 random bytes, shown once at enrollment; only its
  SHA-256 is stored.
- `EnrollmentToken{Hash, User, ExpiresAt, UsedAt}`. Single use, default 24 h.

### Group resolution

`RuleSet` gains `groups: {user: [group, ...]}`, authored in the same YAML and
versioned with the rules. The client never sees claims. An identity-provider
sync later replaces the map without touching clients or the bundle format
(designed 2026-09-22 in `2026-09-22-idp-group-sync-design.md`: a pushed
snapshot that unions with this map rather than replacing it).

### Policy compilation split (`internal/policy`)

- `RuleSet.Slice(groups []string) *Bundle` — server side. Keeps every rule
  that has no group match or whose group match intersects `groups`. Repo
  matchers are kept verbatim.
- `Bundle.Compile(repo string) *Document` — client side.
- `RuleSet.Compile(Subject)` is redefined as `Slice(groups).Compile(repo)`; a
  test proves the result equals the previous implementation on the example
  policy.
- `Bundle{Version, User, Groups, Rules []Rule}`. The same JSON shape is served
  over HTTP and written to disk.

### Store

`PutMachine`, `MachineByCredential(hash)`, `TouchMachine`, `ListMachines`,
`DeleteMachine`, `PutEnrollmentToken`, `ConsumeEnrollmentToken(hash) (user, error)`
— consume is atomic and fails with `ErrConflict` on a used or expired token.
Memory and Postgres implementations, one conformance suite, migration
`0002_machines.sql`.

### API

| Route | Auth | Behaviour |
| --- | --- | --- |
| `POST /v1/enrollment-tokens` `{user}` | admin | 201 `{token, expiresAt}` |
| `POST /v1/machines/enroll` `{token, name, os}` | none | 201 `{machineId, credential}`; 409 on a used/expired token |
| `GET /v1/bundle` | machine | 200 bundle with ETag, 304 on match; **200 empty bundle** when no policy is authored; 401 on a bad credential |
| `GET /v1/machines` | admin | list |
| `DELETE /v1/machines/{id}` | admin | 204; the machine's next fetch is 401 |
| `POST /v1/policy/revisions` | admin | as today, now authenticated |
| `GET /v1/policy?group=&repo=` | none | diagnostic preview, unchanged |

Admin auth is `AWD_ADMIN_TOKEN` as a bearer token. When the variable is unset
the admin routes answer 503 "admin token not configured"; they are never open
by accident. Machine auth is a bearer credential compared in constant time
against the stored hash; `LastSeenAt` is touched on each fetch. TLS is the
deployment's job and is documented, not built.

`awd` CLI gains `enroll-token <user>`, `machines`, `revoke <id>`, all
sending `AWD_ADMIN_TOKEN` as the bearer.

Rule fragments for every agent are validated at apply time through
`policy.ManagedValidator`, as Claude's are today: `awd serve` and `awd apply`
register one validator per adapter (`schema.ForAgent` for Claude, the key
allowlists for Codex and Gemini), so an unknown key fails in front of the
author.

## aw-sync

`cmd/aw-sync`, logic in `internal/sync`. Runs as root. Timer-driven, not a
daemon: `once` is restart-safe and is what MDM tooling expects.

- `aw-sync enroll --server URL --token T [--name host] [--agents claude,codex,gemini]`:
  enrolls, writes `machine.json` `{server, machineId, credential, agents}`
  mode 0600. `--agents` defaults to every registered adapter; a machine
  without Codex installed lists only what it runs, so no directory is
  created for an agent that is not there. Refuses to overwrite an existing
  enrollment without `--force`.
- `aw-sync once`: one cycle, exit 0 or 1.
- `aw-sync status [--json]`: last sync, bundle version, per-agent files with
  hashes, drift, last error, renderer notes.
- Timer units for systemd, launchd and Task Scheduler ship under
  `deploy/aw-sync/`. `install-timer` is deferred.

State lives in a root-owned directory that other users can read, because
`aw doctor` runs as the developer and reports from it:

| | Linux | macOS | Windows |
| --- | --- | --- | --- |
| `machine.json` (0600) and `state.json` (0644) | `/var/lib/agent-wrapper/` | `/Library/Application Support/agent-wrapper/` | `C:\ProgramData\agent-wrapper\` |

Cycle (`sync.Run(ctx, Config) Result`):

1. Load `machine.json`. Missing: error, exit 1.
2. `GET /v1/bundle` with `If-None-Match` from `state.json`. 304: done.
   Network error, 401 or 5xx: keep every file as it is, record the error,
   exit 1. **Rendered files are never deleted on failure.**
3. Run the renderer of every agent named in `machine.json`. Each returns
   `[]agent.File`.
4. Validate every file before writing any. One failure means nothing is
   written this cycle, for any agent.
5. Write all files with `cache.Replace`. TOML files carry a header comment
   naming aw-sync and the revision; JSON files get a sibling `.aw-revision`.
6. Write `state.json` `{etag, version, syncedAt, files: {path: sha256}, error, notes}`
   and append an audit line.

Drift: `status` reports a rendered file whose hash differs from `state.json`.
`once` overwrites drift unconditionally; the files are root-owned and a user
who can edit them already has root.

Ownership: aw-sync owns the whole of Codex's `requirements.toml` and Gemini's
`settings.json`, which have no drop-in directory. Gemini's `policies/` is a
drop-in directory and only `50-agent-wrapper.toml` is touched. Claude's
`aw-bundle.json` is aw-sync's own file.

Paths, one table in `internal/sync/paths.go`, keyed by GOOS:

| Agent | Linux | macOS | Windows |
| --- | --- | --- | --- |
| Claude bundle | `/etc/claude-code/aw-bundle.json` | `/Library/Application Support/ClaudeCode/aw-bundle.json` | `C:\Program Files\ClaudeCode\aw-bundle.json` |
| Codex | `/etc/codex/requirements.toml` | same | `%ProgramData%\OpenAI\Codex\requirements.toml` |
| Gemini | `/etc/gemini-cli/settings.json`, `/etc/gemini-cli/policies/50-agent-wrapper.toml` | `/Library/Application Support/GeminiCli/...` | `C:\ProgramData\gemini-cli\...` |

## Renderers

`agent.Renderer` is an optional adapter interface, like `Inspector`:

```go
Render(bundle *policy.Bundle) ([]agent.File, error)
// File{Path string; Content []byte; Mode fs.FileMode}
```

Paths are relative to a root the caller supplies, so tests render into a
temporary directory. `policy.agents.<name>.managed` is each agent's own
document.

Repo-scoped rules cannot be expressed in a static file. Codex and Gemini
renderers compile with `Repo = ""`, which drops them, and return a note such
as `3 repo-scoped rules are not enforceable for codex`. `aw-sync status`
shows the note. Silent is not an option.

### Claude

Writes two files:

- `aw-bundle.json`: the bundle filtered to rules that mention `claude`, repo
  matchers intact. No compilation here.
- `managed-settings.d/50-agent-wrapper.json`: the static `policyHelper`
  drop-in from `deploy/managed-settings/`, with the helper path for this OS.
  It never changes, but having aw-sync own it means installing governance on
  a machine is "install the binaries, enroll" and nothing else; MDM pushes no
  per-agent files. `refreshIntervalMs` stays at 300000 so a running session
  re-reads the bundle after a sync without a restart.

Coexistence: server-managed settings from the claude.ai console, or a Claude
apps gateway, shadow the helper entirely. An organization uses one channel or
the other; `aw doctor` reports the collision, as it does today.

### Codex

`managed` is `requirements.toml` as a map. Merge across matching rules:
scalars and `allowed_*` lists **replace** (a union would widen an allowlist);
`mcp_servers` merges by name; `rules.prefix_rules` appends. Output through
`github.com/pelletier/go-toml/v2` with a header comment. Validation: a
vendored, dated allowlist of top-level keys from the requirements reference,
plus a TOML round trip. An unknown key is an apply-time error, on the same
path as Claude's schema check.

Coexistence: Codex composes requirements from the system file (ours), then
cloud-managed requirements from a ChatGPT Business or Enterprise workspace,
then MDM, each overriding scalars and lists from the layer below. For an
organization on a ChatGPT plan, a workspace admin's cloud policy therefore
wins over ours on any key both set. Documented, not fought; for API-key
organizations there is no cloud layer and ours is the policy.

### Gemini

`managed` is `{settings: {...}, policies: [{...}]}`. `settings` renders to
`settings.json` (merge: `tools.exclude` and `mcp.allowed` union, `mcpServers`
by name, everything else replaces). `policies` renders to
`policies/50-agent-wrapper.toml`, one `[[rule]]` per entry, appended across
rules. Validation: JSON round trip, a top-level settings key allowlist, and
each policy rule must carry `toolName` and `decision`.

Known weakness: Gemini reads the system settings path from
`GEMINI_CLI_SYSTEM_SETTINGS_PATH`, which a user can export to point elsewhere.
The admin `policies/` directory has no such override. Gemini's own enterprise
documentation recommends a wrapper script that pins the variable; the
launch-time `aw gemini` adapter in a later milestone is that wrapper. Until
then, put the rules that matter in `policies`, and treat `settings.json` as
defaults.

## aw-policy, offline

`policyhelper.Run` becomes:

1. Read `<system dir>/aw-bundle.json` (root-owned, 0644; it is the
   organization's policy, not a secret). `AW_POLICY_BUNDLE` overrides the
   path for tests (test builds only; see the 2026-09-22 hardening design).
2. Missing or unparseable: `{}` envelope, exit 0, note. With `requireBundle`
   in `aw-policy.json`: exit 1. The configuration file is otherwise
   unnecessary.
3. `repo.Detect(cwd)` → `bundle.Compile(repo)` → managed settings for
   `claude` → `schema.Validate` → emit. A validation failure emits `{}` with
   a note: the bundle was validated when written, so this only guards a
   newer schema in this binary.
4. Audit line, as today.

Deleted: the HTTP fetch, ETag handling, the per-subject cache, and
`serverUrl`, `groups` and `timeoutMs` in `aw-policy.json`. Kept: envelope
rules, the 1 MiB guard, panic recovery, audit, `AW_POLICY_CONFIG`.

`aw doctor`'s Claude inspection adds the bundle: present, version, age from
aw-sync's `state.json`, and aw-sync's last error.

## Failure modes

| Where | Failure | Behaviour |
| --- | --- | --- |
| aw-sync | server down, 401, malformed bundle | keep files, record error, exit 1, timer retries |
| aw-sync | any renderer fails validation | write nothing for any agent this cycle |
| aw-policy | no bundle | `{}`, exit 0; exit 1 with `requireBundle` |
| aw-policy | bundle fails schema | `{}` with a note |
| awd | no policy authored | 200, empty bundle |
| awd | `AWD_ADMIN_TOKEN` unset | admin routes 503 |

## Testing

- `policy`: `Slice` then `Compile` equals the previous `Compile` on the
  example policy; group resolution from the `groups` map.
- store conformance: machines; enrollment tokens are single use and expire;
  Postgres migration applies.
- handler: enroll succeeds once and 409s after; bad credential 401; bundle
  ETag and 304; admin routes 401 with a wrong token and 503 with none.
- renderers: golden files per agent under `testdata/`; merge rules; the
  repo-scoped drop note; TOML and JSON round trips.
- `sync`: fake awd via `httptest`; all-or-nothing writes; 304 is a no-op; a
  failed fetch leaves files untouched; drift detection; enrollment writes
  0600.
- end to end: build `awd`, `aw-sync`, `aw-policy`; enroll; sync into
  temporary roots; `aw-policy` with `AW_POLICY_BUNDLE` emits a repo-specific
  policy; stop `awd`; sync exits 1 with files intact; `aw-policy` still
  emits.
- The fake-server `aw-policy` tests from milestone 3 are removed with the
  code they test.

## Sequencing

One commit each, each leaving the suite green:

- **M4a** — `groups` and `Slice`/`Bundle` in `policy`; machines and
  enrollment in store and handler; admin auth; `awd enroll-token`,
  `machines`, `revoke`.
- **M4b** — Claude renderer; `aw-sync enroll`, `once`, `status`; timer units
  under `deploy/aw-sync/`. The milestone-3 `aw-policy` keeps fetching from
  the network until the next commit, so the README flow never breaks.
- **M4c** — `aw-policy` offline rewrite; doctor bundle finding; README.
- **M4d** — Codex renderer and validator; example policy `codex` block.
- **M4e** — Gemini renderer and validator; example policy `gemini` block.

## Out of scope, deliberately

- Launch-time `aw codex` / `aw gemini` adapters for per-repo overlays
  (advisory, bypassable). A later milestone.
- Identity-provider group sync. The `groups` map is the seam it plugs into.
- Signed bundles. Machine credential over TLS is the v1 integrity story.
- The semantic (Jev) decision layer. Unchanged: deferred, opt-in, pending the
  vendor's self-hosting answer.
- Broker and budgets. The Claude apps gateway covers them for Claude; the
  other agents' vendors have their own.
