# Platform refresh — plan

Spec: `docs/superpowers/specs/2026-10-23-platform-refresh-design.md`. Base: `bd63111` on branch `platform-refresh`.

## Global constraints

- Go commands use `GOTOOLCHAIN=auto` (local toolchain is 1.24.0). Never run plain `go mod tidy`; use `GOTOOLCHAIN=local go mod tidy -e` only if a dependency changes (none should).
- `go.mod` stays `go 1.24`.
- No unit test (untagged) may touch the network. All network code lives in `internal/drift` behind `//go:build drift`.
- Failure-mode rows from the spec that need a test: overlay key already in vendored schema → test failure; overlay parent unresolvable → test failure; overlay value of wrong type rejected; typo in a strict block still rejected; removed Codex key rejected naming the new date.
- Commit trailers are handled by the hook; don't amend for trailers.

## Parallel group A (no shared files) — dispatch together, each in a worktree

### T1 — build stack (agent: sdd-implementer-lite)
Files: `deploy/Dockerfile`, `.github/workflows/console.yml`, `.github/workflows/web.yml`.
- Dockerfile: `golang:1.27-alpine` build stage, `alpine:3.22` runtime. Nothing else changes.
- Both workflows: `node-version: 24`.
- `console.yml`: add a step `docker build -f deploy/Dockerfile .` (GitHub-hosted ubuntu runners have Docker).
Checks: `docker build -f deploy/Dockerfile -t awd-check .` exits 0 locally (AC1); `grep -n "node-version: 24"` hits both workflows (AC7 part).

### T2 — Codex requirements keys (agent: sdd-implementer)
Files: `internal/agent/codex/requirements/keys.go`, its tests.
- Re-read the `requirements.toml` ConfigTable in `https://learn.chatgpt.com/docs/config-file/config-reference.md` (download to the scratchpad, don't trust memory). Candidate additions from the audit, each to be confirmed as a top-level key: `additional_developer_instructions allow_browser_and_computer_use allow_login_shell application apps auto_review check_for_update_on_startup enforce_residency feedback guardian_extra_policy in_app_browser log_dir model_catalog_json model_provider model_providers models plugins sqlite_home windows`. Candidate removal: `allow_locked_computer_use`.
- `KeysAsOf = "2026-10-23"`; list stays sorted.
Tests: `TestKeysAcceptsOctober2026Additions` (table over every added key validates), `TestKeysRejectsRemovedKeyNamingDate` (`allow_locked_computer_use` → error contains `2026-10-23`), existing sortedness test (add one if missing: `TestKeysSorted`).

### T3 — Claude schema overlay (agent: sdd-implementer)
Files: `internal/agent/claude/schema/overlay.go` (new), `schema.go` (`load()` applies overlay before `AddResource`), tests.
Interface:
```go
// KeysAsOf: date the overlay was read from the Claude Code settings reference.
const KeysAsOf = "2026-10-23"
type Extra struct {
    Path   string         // dotted settings path, e.g. "sandbox.allowNestedUserNamespaces"
    Schema map[string]any // JSON schema for the value, e.g. {"type": "boolean"}
}
var Overlay []Extra
func apply(doc map[string]any, extras []Extra) error // walks properties, follows local "#/..." $ref
```
- `apply` errors if the parent path doesn't resolve to an object schema with `properties`, or if the key already exists (stale overlay).
- Seed: download `https://code.claude.com/docs/en/settings-reference.md`; list every dotted key whose top-level block is one of the strict blocks (`attribution permissions hooks statusLine fileSuggestion sandbox spinnerVerbs spinnerTipsOverride worktree autoMode subagentStatusLine voice policyHelper`) and is missing from the vendored schema. Give each the type the reference states; skip (and list in the report) any whose type is unclear. Hook *event names* under `hooks` count as keys.
Tests: `TestOverlayAcceptsNestedUserNamespaces`, `TestOverlayRejectsWrongValueType` (`"yes"`), `TestStrictBlockStillRejectsTypo` (`permissions.denny`), `TestApplyRejectsExistingKey`, `TestApplyRejectsUnresolvableParent`, `TestOverlayEntriesAllApply` (compiles with the real overlay).

### T5 — positioning copy (agent: sdd-implementer-lite)
Files: `README.md` (section "What remains uncovered", ~line 18), `web/content/home.ts` ("Rules per group on seat plans" card), `web/content/site.ts` only if needed.
- State: per-group gap = Claude Code on claude.ai seats (console org-uniform; gateway needs an upstream credential). Codex on ChatGPT plans has per-group policy in OpenAI's Agent Security; aw's value for Codex is per-repository rules, one policy across agents, API-key users, self-hosting.
- Keep card count and shape; plain words, no superlatives.
Checks: `pnpm -C web test` passes (AC8); no sentence claims vendors in general cannot target groups.

## Group B (after T2 and T3 merge)

### T4 — drift job (agent: sdd-implementer)
Files: `internal/drift/drift_test.go`, `internal/drift/parse_test.go` (both `//go:build drift`), `.github/workflows/drift.yml`.
- Pure parse/diff helpers tested offline from fixture strings: `TestDiffNames` (one key removed from a pinned list → failure message names it and the file to edit), parsers for each source.
- Live tests: `TestCodexRequirementsKeys` vs `requirements.Keys`; `TestGeminiSettingsKeys` vs `managed.SettingsKeys` (note `policyPaths: pathArraySetting(` form) and `TestGeminiDecisions` vs `managed.Decisions`; `TestClaudeNestedKeys` vs vendored schema ∪ `schema.Overlay`. Unparseable page → `t.Fatalf("upstream page changed shape: …")`.
- Workflow: weekly `schedule` + `workflow_dispatch`, `permissions: contents: read`, `go-version: stable`, runs `go test -tags drift ./internal/drift/`.
Checks: AC5, AC6 (`go list ./internal/drift/` without tag reports no Go files / build-constraint exclusion; `go test ./...` passes offline).

## Ledger

```
```

## Backlog

- Push aw policies into OpenAI's policy API (author once for Codex).
- Deliver Claude policy as an organization mod (`prependPlugins`, `tool.check`).
- Next 16.3.6 → 16.4.x; frontend major bumps on Node 24.
- Rebuild the deleted design doc from specs + memory.
