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
- Removed-agent hint: public docs URL instead of repo path once a docs site exists (panel-cdo nit).
- `TestRunRootChangeNeverLosesAnAgentsFiles`: name claims more than it checks; override A to B prunes A (panel-security nit).
- Prune: refuse a parent dir that is not root-owned, or unlinkat via O_NOFOLLOW dir fd (panel-security nit; mitigated by no prune under --root).
- .agents/product-marketing.md still lists Gemini CLI and `aw gemini` (D docs session).

## Ledger

PLAN b477848 spec approved (owner 2026-10-26: hard remove soft landing, prune if unchanged, isolation others proceed, worktrees removed)
USAGE start 0
T3 done a0104bd sdd-implementer-lite helper + agy nits; AC9 grep clears after T1 deletes gemini
T3 review approve a0104bd sdd-reviewer-lite nit: dropped_test compares got[0] only
T4 done 258071d sdd-implementer-lite docs+site; open: web/lib/seo.ts, opengraph-image.tsx, demo/schema.ts, architecture SVGs still Gemini
T4 review approve 258071d sdd-reviewer-lite should: apply-error sample hint must match T1 (contract fixes it, check at merge); nit removing.md Gemini paths kept
T5 done 2915fca sdd-implementer-lite web meta, OG, schema, SVGs; email.test.ts updated; web/content handled by T4
T1 done 4db3165 sdd-implementer gemini removed; extra files drift tests, awd validator, examples/org-policy.yaml (also T4: merge conflict likely); launch msg '<hint>; run it with aw agy'
T5 review approve 2915fca sdd-reviewer-lite should: docs/assets/architecture.json source still says Gemini CLI (coord fix after merge)
T1 review approve 4db3165 sdd-reviewer nits: revision.go:8 long line; .agents/product-marketing.md still lists Gemini
MERGE group A 02c767d T1,T3,T4,T5 clean; go vet+test green; AC1 AC9 pass
T5 coord 37b5c0f architecture.json label Codex, agy; revision.go comment reflow
COORD review approve 37b5c0f sdd-reviewer-lite architecture.json + revision.go
T2 done 1005189 sdd-implementer prune+isolation+304 prune+enroll keeps Files; sidecar is .aw-revision; concerns: 304 unknown-root agent files = orphans, stale Run doc comment
T2 fix 1005189 sdd-reviewer fix-needed root-less failed agent's files pruned (D3), test seeds none, stale Run doc
T2 done 6918021 sdd-reviewer-lite re-review approve; should: root-less cycle skips removed-agent prune too (intended, spec D3 note)
T2 coord 2dd5a40 spec D2 states root-less cycle skips all prune (reviewer-lite should)
PANEL design 2dd5a40 must=0 should=4 nit=4
PANEL cdo 2dd5a40 must=0 should=5 nit=4
SEAT panel-cdo fail 2dd5a40 AC11 sync note no re-enroll step, paid-licence exception missing, sample error clipped
PANEL security 2dd5a40 must=0 should=1 nit=1
SEAT panel-security pass 2dd5a40 AC12 caveat: --root override prunes enrolled agent's real files on 304
PANEL correctness 2dd5a40 must=1 should=2 nit=3
TRIAGE fixed=22 dismissed=0 backlog=1
FIX wave 4633631 sdd-implementer F1-F17; F1 narrowed: skip prune when a Roots override replaces a default root holding recorded files; no test F10/F9 note
COORD fix-wave re-review 4633631 sdd-reviewer-lite fail: should 304 loop under shadowing Roots override (forced full cycle forever, wrong note); should F10 doctor untested; nits spec RemovedAgents name
PANEL security 4633631 must=0 should=0 nit=2
SEAT panel-security pass 4633631 AC12 re-run on fix diff
PANEL cdo 4633631 must=0 should=1 nit=3
SEAT panel-cdo fail 4633631 AC11 re-enroll command in sync note not runnable (needs --server + new token, literal ...)
COORD owner approved fix wave 2 past cap; AC11 owner at gate; nits folded: drop aw agy apply/enroll, diagram label; docs URL backlog
FIX wave 158698c sdd-implementer wave 2 G1-G6; SVGs sed-patched not regenerated
COORD owner: AC12 after wave 2 = reviewer-lite regression check + owner at gate (security seat re-run cap used)
COORD fix-wave re-review 158698c sdd-reviewer-lite fail: must SVG </text> corrupted by sed; AC12 regression none
COORD ac7a1d0 repaired </text> in both SVGs, xml.dom parse ok
COORD review approve ac7a1d0 sdd-reviewer-lite SVGs parse, only agy label lines changed
CONVERGE 90b3f04 spec-converge AC1-10 VERIFIED; AC11, AC12 INCONCLUSIVE owner at gate
SEAT owner pass 68bdfeb AC11
SEAT owner pass 68bdfeb AC12
PR #12 opened https://github.com/alchristleo/agentic-warden/pull/12

## Retro (2026-10-26)

- **Numbers:** tasks=5 fix_rounds=1 coord=2 fix_waves=2 blocked=0 seats pass/fail=2/2 reruns=2 owner=2 panel(must/should/nit)=[design 0/4/4; cdo 0/5/4; security 0/1/1; correctness 1/2/3; security(re-run) 0/0/2; cdo(re-run) 0/1/3] triage=[fixed=22 dismissed=0 backlog=1]
- **Removal leaked past its task:** T4 left Gemini in web meta, OG image, schema, SVGs (owner added T5); `.agents/product-marketing.md` still leaks. Fix: removal/rename tasks take a repo-wide grep with an explicit allowlist as their acceptance test.
- **Docs command not runnable:** panel-cdo failed AC11 twice (no re-enroll step, then literal `...` and missing `--server`/token), forcing fix wave 2 past cap. Fix: implementer rule, every user-facing command is copy-pasteable with real flags.
- **sed broke SVG `</text>`:** caught by reviewer-lite, coord repair ac7a1d0. Fix: CI step parses every committed SVG (T1 of next plan).
