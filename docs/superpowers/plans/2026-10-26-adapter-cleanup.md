# Adapter cleanup: plan

Spec: `docs/superpowers/specs/2026-10-26-adapter-cleanup-design.md`. Branch `adapter-cleanup`.

Shared contract (all tasks): `internal/policy/removed.go`

```go
// RemovedAgents maps an agent key agent-wrapper no longer supports to the hint shown to the user.
var RemovedAgents = map[string]string{
	"gemini": "Gemini CLI is no longer supported by agent-wrapper; use agy (Antigravity CLI)",
}
```

Parallel group A: T1, T3, T4 (no shared files). T2 after T1.

## T1 Gemini removal (sdd-implementer, depends-on: none)

Files: delete `internal/agent/gemini/**`; add `internal/policy/removed.go` (+ test); `internal/policy/ruleset.go`,
`internal/policy/compile.go`, `internal/policy/policy.go` comment, `internal/agent/revision.go` comment,
`internal/sync/paths.go` (+ paths_test), `cmd/aw/main.go` (+ tests), `cmd/aw-sync/main.go` (enroll + usage line only),
`cmd/awd/main.go` comment, any `_test.go` that imports gemini (`cmd/aw/e2e_test.go`, `cmd/aw-sync/e2e_test.go`,
`internal/sync/umask_unix_test.go`).
Interfaces: `policy.RemovedAgents` (above). Ruleset validation error:
`rule %q: agent %q was removed: delete the %s block from this rule; <hint>`.
`aw <removed>` → exit nonzero, stderr `aw: <hint>` (contains `aw agy`; adjust hint wording for launch:
`...; use aw agy`). `aw-sync enroll --agents gemini` → `agent "gemini": <hint>`.
Tests:
- `TestValidateRejectsRemovedAgent` (internal/policy): rule with `agents: {gemini: {...}}` → error contains rule name and `agy`. (AC2)
- `TestEnrollRejectsRemovedAgent` (cmd/aw-sync): refused before the token is spent (no HTTP call to fake server). (AC3)
- `TestLaunchRemovedAgent` (cmd/aw): exit nonzero, message contains `aw agy`. (AC4)
- `TestOldBundleGeminiKeyIgnored`: a bundle with a gemini rule still compiles/renders for claude, codex, agy (Compile + each Render) and aw-policy's decision path does not error.
Constraints: AC1 grep must pass (`git grep -i gemini -- '*.go' ':!*_test.go' ':!internal/policy/removed.go'` empty).
Do not touch internal/sync/sync.go (T2).

## T2 aw-sync prune + isolation (sdd-implementer, depends-on: T1)

Files: `internal/sync/sync.go`, `internal/sync/files.go` (if helpers), `internal/sync/sync_test.go`,
`cmd/aw-sync/main.go` (enroll --force carries Files), `cmd/aw-sync/e2e_test.go`.
Interfaces: no exported API change. Internal: `prune(prev map[string]string, keep func(path string) bool) (removed []string, notes []string)`;
under-root check via `filepath.Rel` + `filepath.IsLocal`.
Behavior: spec D1 (removed agent in machine.Agents → note `<name>: <policy.RemovedAgents hint>`, skip), D2, D2a, D3, D4.
Tests (fixtures use real renderer outputs: claude managed-settings.json, codex requirements.toml, recorded Gemini paths
`/etc/gemini-cli/system-settings.json`-shaped under a temp root, with `.revision` sidecars as sync.go writes them):
- `TestRunSkipsRemovedAgentAndPrunes` (AC5)
- `TestRunPruneOn304` (AC6a): fake server 304, orphans removed, state.Files updated, enrolled agents' files untouched.
- `TestRunPrune*` ≥ 4 (AC6): unchanged+sidecar removed; edited kept+note+untracked; symlink kept+note; missing untracked silently;
  non-absolute/unclean candidate skipped+note; state-dir files never removed; sidecar kept when main kept.
- `TestRunIsolat*` ≥ 4 (AC8): render failure; write failure (read-only dir); unknown agent in machine.json; next cycle refetches (no If-None-Match) and retries. Each asserts other agent written, failed agent's old files on disk and in state.Files, Err names agent, state.ETag empty.
- `TestEnrollForceKeepsFiles` (unit) and `TestReenrollDropsAgentFiles` (e2e, real binaries, AC7).
Constraints: state-dir write failure still fails whole cycle. Audit line keeps today's shape.

## T3 Shared DroppedRepoRules + agy nits (sdd-implementer-lite, depends-on: none)

Files: `internal/agent/dropped.go` (+ test), `internal/agent/codex/render.go`, `internal/agent/agy/render.go`,
agy Inspect test, agy header-value secret check (+ test).
Interface: `func DroppedRepoRules(bundle *policy.Bundle, agentName, file string) []string` → nil when none, else
`"<agent>: N repo-scoped rule(s) not enforceable in <file>: a, b"` (byte-identical to today's codex/agy notes).
Tests: `TestDroppedRepoRules` (nil bundle, none, two rules, other agent's rule ignored); existing codex/agy golden/render tests unchanged and green;
agy Inspect test asserts the specific Error message; header check: `" Bearer x"`, `Token x`, `Digest x` rejected.
Constraints: AC9.

## T4 Docs and site (sdd-implementer-lite, depends-on: none)

Files: README.md, docs/site/*.md, deploy/aw-sync/README.md, examples/org-policy.yaml, web/content/{site,home,pricing}.ts,
web tests that assert Gemini text (web/lib/demo/*, web/e2e/home.spec.ts) if they break.
Do: drop Gemini as a supported agent everywhere; one "Gemini CLI (removed)" entry in docs/site/reference.md: why
(Gemini CLI retired 2026-06-18 for most users, https://www.theregister.com source already cited in the agy docs), migrate to agy,
what happens to existing rules (apply rejects until the block is deleted; stored policies keep working, Gemini rules ignored)
and enrollments (gemini skipped with a note; aw-sync removes the Gemini files it wrote if unchanged; re-enroll without gemini).
README says `aw agy` is a thin launcher. README:82 drop inner "(end of life)"; how-it-works:90 hooks parenthetical below the list.
Checks: AC11 commands; `cd web && pnpm test` if web/ files changed.

## T5 Web meta + diagram leftovers (sdd-implementer-lite, depends-on: none; owner added 2026-10-26)

Files: web/lib/seo.ts, web/app/opengraph-image.tsx, web/lib/demo/schema.ts (+ web tests asserting them),
docs/assets/architecture-light.svg, docs/assets/architecture-dark.svg.
Do: drop Gemini CLI as a supported agent; supported set = Claude Code, Codex, agy (Antigravity CLI). SVGs: replace the
Gemini node/label with agy, keep layout, both themes consistent. If schema.ts gemini is a demo policy key, switch it to agy.
Checks: `cd web && pnpm test`; `grep -c -i gemini` on the five files prints 0 (except agy model IDs `gemini-3.8-*`).

## Backlog

- Known-agent allowlist for rule keys (typo'd agent keys are accepted silently).
- Merged-document validation for every agent at `awd apply` (A hardening).
- Skip-with-note for a foreign agy admin file (Renderer contract).
- agy permissions/MCP/hooks if Google adds a system tier; macOS/Windows real-agy checks.
- Back-off for an agent whose render fails every cycle.

## Ledger

PLAN b477848 spec approved (owner 2026-10-26: hard remove soft landing, prune if unchanged, isolation others proceed, worktrees removed)
USAGE start 0
T3 done a0104bd sdd-implementer-lite helper + agy nits; AC9 grep clears after T1 deletes gemini
T3 review approve a0104bd sdd-reviewer-lite nit: dropped_test compares got[0] only
T4 done 258071d sdd-implementer-lite docs+site; open: web/lib/seo.ts, opengraph-image.tsx, demo/schema.ts, architecture SVGs still Gemini
