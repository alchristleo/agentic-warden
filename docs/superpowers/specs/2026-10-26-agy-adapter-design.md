# Antigravity CLI (agy) adapter: design

Date: 2026-10-26. Branch: `agy-adapter`. Size: Large (new agent, new root-owned file, docs).

## Goal

Govern Google Antigravity CLI (`agy`), the successor of Gemini CLI (sunset 2026-06-18), with the one
control agy enforces machine-wide: the root-owned `admin_settings.json` (LLM gateway, CA bundle,
approved models). aw-sync renders it from the signed bundle, `aw doctor agy` reports whether it is in
place, `aw agy` launches agy with the policy's env, and the docs say plainly what agy cannot enforce.

## Non-goals

- Permission allow/ask/deny rules, MCP servers, hooks for agy. They live only in the developer's home
  (`~/.gemini/antigravity-cli/settings.json`, `~/.gemini/config/{hooks,mcp_config}.json`) with no system
  tier and no path override, so aw cannot enforce them. No advisory writes into home config (owner, 2026-10-26).
- Repository-scoped agy rules: the admin file is machine-wide. Dropped with a note, as for Gemini.
- Removing the Gemini adapter (Backlog).
- Carrying the gateway API key in a policy (owner, 2026-10-26).
- Gemini Enterprise console controls (server-side, Google's).
- macOS and Windows paths are rendered from F1 but verified only by unit test; real-agy verification is Linux.
- Per-agent isolation of a failed write in aw-sync (all-or-nothing stays).

## Facts (verified 2026-10-26, agy 1.4.7, docs.cloud.google.com enterprise-gateway page, antigravity.google/docs)

- F1 Admin file path: Linux `/etc/antigravity/admin_settings.json`, macOS
  `/Library/Application Support/Antigravity/admin_settings.json`, Windows
  `%ProgramData%\Antigravity\admin_settings.json`. Google's install: root-owned, mode 0644.
- F2 Documented keys: `gateway{enabled, url, apiKey, wireProtocol: genai|openai, customHeaders}`,
  `caCertPath`, `models{defaultModelId, list:[{modelId}]}`. The binary's JSON tags match.
- F3 Precedence: admin file > `AGY_LLM_GATEWAY_*` env > `~/.antigravity/settings.json`, merged per field;
  developers cannot override admin values.
- F4 No env var or flag moves the admin file (strings of the 1.4.7 binary: only the fixed per-OS path).
  So `aw agy` cannot scope it per launch.
- F5 Unknown: what agy does with an unparseable file, an unknown key, `{}`, or a bad field
  (the binary has "invalid admin settings: %w" and "json: unknown field %q"), and when it ignores the
  file ("admin controls not applicable" exists: maybe under non-enterprise sign-in). `pending: T1`.
- F6 The binary also reads `AGY_LLM_GATEWAY_{HEADERS,CA_CERT,MODELS,PROXY_URL,TIER_PRO,TIER_FLASH,WIRE_PROTOCOL}`;
  the documented admin keys are only F2. Undocumented admin keys are not supported.

## Decisions

- D1 Agent key and command: `agy` (owner). Package `internal/agent/agy`.
- D2 Policy shape: `agents.agy.managed` is the admin_settings.json object itself. Shapes considered:
  (a) verbatim object (chosen: no translation layer, docs map 1:1 to Google's); (b) aw-specific keys
  (`gatewayURL`, `models`) translated on render (rejected: a second vocabulary to learn and to keep
  in step with Google).
- D3 Validation (`awd apply` and Render, one function `managed.Validate` in `internal/agent/agy/managed`): dated allowlist from F2
  (stale by design, as Gemini's: a new Google key needs an aw release; typos fail loudly instead of being ignored);
  unknown key at any level → reject; `gateway.apiKey` → reject with a message naming
  `AGY_LLM_GATEWAY_API_KEY` (owner); `customHeaders` name matching `(?i)auth|key|token|secret|cookie|password`
  → reject, same reason (best effort, documented as such); `wireProtocol` ∈ {genai, openai}; `gateway.url` https only (Google documents an HTTPS endpoint);
  `gateway.enabled: true` requires `url`; keys and model IDs match case-sensitively;
  `caCertPath` absolute; `models.list[].modelId` non-empty string; `defaultModelId`, if set with a
  list, is in the list. `launch` on agy → reject (as Gemini).
- D4 Merge across rules: default rules (objects merge per key, a later rule wins per key, arrays replace).
  `models.list` is an allowlist, so a union would widen it. `gateway.customHeaders` is an object, so headers
  from several rules accumulate, a later rule winning per header name.
- D5 Render: one file, `admin_settings.json`, mode 0644, owned whole by aw-sync, revision in the
  `.aw-revision` sibling (JSON has no comments). Empty policy → `{}` (stale file never outlives its policy).
  `pending: T1`: T1 must show agy 1.4.7 starts with `{}` before any render task starts; if it does not,
  the empty case renders a file agy accepts or the spec returns to the owner.
- D6 `aw agy`: passthrough. Merge policy env (same `merge.Env` as other agents), pass args, note
  `agy: enforcement is machine-wide (admin_settings.json via aw-sync); nothing is pinned per launch` (owner).
- D7 `aw doctor agy` (Inspector): admin file missing → warn; unparseable or fails Validate → error;
  no `.aw-revision` sibling → warn (file not written by aw-sync); parseable with sibling → ok worded
  "admin file present, revision X" (never "enforced": agy may ignore it, F5); `gateway.enabled` with no
  `AGY_LLM_GATEWAY_API_KEY` in env → warn (agy fails with "gatewayAuth: ... is not set"); any `AGY_LLM_GATEWAY_*` in env → ok-level note that the admin file outranks it (F3).
- D8 aw-sync: `agy` is renderable. Whether a default `aw-sync enroll` includes it: `pending gate: Q1`
  (aw-sync overwrites unconditionally, so a Google- or MDM-installed admin file would be replaced).
  Machines already enrolled keep their stored list. aw-sync runs as root; an unwritable path fails the
  whole cycle, as for every agent today (Renderer contract, all-or-nothing); not changed here.
- D10 Launch notes print only in `aw doctor`, so the D6 note never appears on a normal launch.
- D9 Gemini: adapter unchanged; docs mark it deprecated (Gemini CLI end of life), pointing to agy.

## Design

- `internal/agent/agy/agy.go`: `Name = "agy"`, `SystemDir(goos)` (F1), `Adapter{Binary, SystemDir}`,
  `Locate`, `Build` (D6), `Inspect` (D7).
- `internal/agent/agy/render.go`: `AdminFile = "admin_settings.json"`, `Render` (D5), repo-scoped rule note.
- `internal/agent/agy/managed`: `Validate(doc)`, `ForAgent(agent, doc)` for awd's `managedValidator`.
- Wiring: `cmd/aw/main.go` adapter list, `cmd/aw-sync/main.go` adapter list + usage line,
  `internal/sync/paths.go` `AgentRoot`, `cmd/awd/main.go` `managedValidator`, `internal/policy`
  `launch` rejection for agy.
- `scripts/probe-agy-admin.sh <admin.json>`: runs agy 1.4.7 as root in a throwaway container with the
  file at `/etc/antigravity/admin_settings.json` against a stub gateway, prints what agy did (T1).
- Docs: `docs/site/how-it-works.md` agy section (what is enforced, the F-list of what is not, gateway key
  via env), `docs/site/reference.md` (`agents.agy.managed` keys), `write-first-policy.md` example,
  `threat-model.md` design limit line, Gemini deprecation line, README agent list.

## Acceptance criteria

1. Render of a full fixture policy produces exactly the golden `admin_settings.json` (mode 0644) and
   `{}` for a bundle with no agy rules; `SystemDir` gives F1's path per OS: `go test ./internal/agent/agy -run 'TestRender' -v`.
2. Validate rejects each of: unknown top-level key, unknown nested key, `gateway.apiKey`, custom headers `Authorization`, `X-Api-Key` and `Cookie`,
   `enabled: true` without `url`, `wireProtocol: "grpc"`, `http://` gateway url, relative `caCertPath`,
   `defaultModelId` not in `list`; accepts the golden fixture: `go test ./internal/agent/agy/managed -v`.
3. `awd apply` of a policy with `gateway.apiKey` for agy exits non-zero naming `AGY_LLM_GATEWAY_API_KEY`,
   and with `launch` on agy exits non-zero: `go test ./cmd/awd -run 'Agy' -v`.
4. Two rules setting `models.list` compile to the later rule's list only, and two rules' `customHeaders`
   accumulate (later wins per name):
   `go test ./internal/policy -run TestCompileAgyModelsListReplaces -v`.
5. A repo-scoped agy rule is not rendered and Render returns a note naming it:
   `go test ./internal/agent/agy -run TestRenderNotesTheRepoScopedRulesItDropped -v`.
6. aw-sync e2e: enroll with `--agents claude,codex,gemini,agy`, `once` writes `<root>/admin_settings.json`
   with the bundle's content and an `.aw-revision` sibling naming the revision; an outage leaves it in
   place: `go test ./cmd/aw-sync -run 'TestEnrollOnceStatusAndOutage' -v`.
7. `aw agy` Build: policy env merged, args passed through in order, D6 note present, no files written:
   `go test ./internal/agent/agy -run TestBuild -v`.
8. `aw doctor agy` reports each D7 case at its level: `go test ./internal/agent/agy -run TestInspect -v`
   and `go test ./cmd/aw -run TestDoctorReportsHowTheAgentIsGoverned -v`.
9. Real agy 1.4.7 as root reads the golden file: with the stub gateway it sends its model request to the
   gateway url with the custom header, and `agy models` lists only the policy's models; agy starts with `{}`; T1's findings on
   F5 are recorded in this spec: `scripts/probe-agy-admin.sh internal/agent/agy/testdata/admin_settings.golden.json`
   (local only, needs docker and root in a container; not reproducible in CI; `check: seat panel-correctness`
   reads the recorded output).
10. Docs: the agy section names every non-goal control (permissions, MCP, hooks, repo scope) as not
    enforced and says how the gateway key reaches developers; Gemini marked deprecated; `for f in README.md docs/site/*.md; do bash scripts/check-links.sh "$f" || exit 1; done &&
    bash scripts/check-readme.sh` pass; wording `check: seat panel-cdo`.
11. Full suite green on the Go floor and stable: `go test ./... && GOTOOLCHAIN=go1.27.3 go test ./...`.

## Security notes

- New writer of a root-owned file that routes all agy LLM traffic and names a CA bundle. Input is the
  signed bundle, validated by D3 both at apply and at render (render failure = nothing written that cycle).
- A policy author can point a fleet's agy traffic at any https gateway and trust any CA path on disk:
  equal to existing policy-write power (threat-model "policy write = fleet code exec"); documented.
- No secrets in policy: `apiKey` and auth headers rejected (D3). Policy `env` could still carry
  `AGY_LLM_GATEWAY_API_KEY`; same as any env secret today; docs say don't.
- File is world-readable by design (agy runs as the developer).
