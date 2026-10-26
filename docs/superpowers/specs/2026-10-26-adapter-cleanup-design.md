# Adapter cleanup: design

Date: 2026-10-26. Branch: `adapter-cleanup`. Size: Large (removes an agent, aw-sync deletes
root-owned files, docs and site).

## Goal

Drop the Gemini CLI adapter (Gemini CLI retired 2026-06-18 for most users; agy is its successor)
without breaking a fleet that still has `gemini` rules or enrollments. Make aw-sync remove the files
it wrote for an agent that is no longer synced, and stop one agent's failure from blocking the others.
Share the one repo-scoped-rule note helper that codex and agy copy.

## Non-goals

- A known-agent allowlist for rule keys (any other unknown key is still accepted, as today). Backlog.
- Merged-document validation for every agent at `awd apply` (A hardening milestone).
- Skip-with-note for a foreign agy admin file (needs a Renderer contract change). Backlog.
- agy permissions/MCP/hooks (no system tier exists). macOS/Windows real-agy checks.
- Removing Gemini rules from policies already stored on the control plane: they stay, aw-sync ignores them.
- Parent-directory symlink swaps and hash-to-remove TOCTOU during prune: agent roots and the state dir are
  root-owned; a local root can do worse. Only the final path component is Lstat-checked.
- Files written by an aw-sync too old to record them in state.json: never pruned.
- Back-off for an agent whose render fails every cycle: the timer cadence bounds it to one GET per tick.
- Removing the now-empty Gemini system directory (`/etc/gemini-cli` etc.): only files aw-sync recorded go.

## Decisions

- **D1 Gemini removal = hard remove, soft landing** (owner, 2026-10-26). `internal/agent/gemini/**` and
  every registration go. One unexported table in `internal/policy/removed.go`, read through `policy.RemovedHint` (`"gemini"` → migration
  hint naming agy), is the only place the name survives in non-test Go code. Callers:
  - `awd apply` (ruleset validation) rejects a rule with a removed agent key; the error names the rule and agy.
  - `aw-sync enroll --agents gemini` is refused with the same hint.
  - `aw-sync once` with `gemini` in machine.json skips it with a note (`gemini: removed from agent-wrapper; ...`)
    and syncs the other agents; the cycle succeeds. Its recorded files are pruned (D2).
  - `aw gemini` exits nonzero with the hint (`use aw agy`).
  Shapes considered: deprecate-for-one-release (owner rejected: two passes), strict error (owner rejected:
  every enrolled machine would fail sync until re-enrolled).
- **D2 Prune = remove if unchanged** (owner, 2026-10-26). After a cycle's writes, every path in the previous
  `state.Files` that this cycle neither planned nor kept (D3) is a prune candidate. Generic: covers an agent
  dropped by re-enroll, a removed agent, and a renderer that stops emitting a file. A candidate is removed
  only if `Lstat` says regular file and its SHA-256 equals the recorded hash; its `.revision` sidecar goes with
  it. A missing file is dropped from state silently. An edited file, a symlink or a non-regular file stays,
  untracked from then on, with a note naming the path. A remove error is a note, not a cycle failure.
  Paths outside a prune candidate set are never touched. A candidate that is not absolute and clean
  (`filepath.IsAbs`, `filepath.Clean(p) == p`) is skipped with a note. State-dir files (bundle, signature, trust,
  etag) are never candidates. A `.revision` sidecar is removed only together with its main file, only if
  Lstat says regular file; it is not hash-checked (its content is a revision line).
  "Under root R" is `filepath.Rel(R, p)` local, not a string prefix.
  If any enrolled agent not removed has no resolvable root, its files cannot be told apart from
  orphans, so prune is skipped for that whole cycle (removed agents included) with one note; recorded files carry over.
  Sidecar suffix is `.aw-revision`.
- **D2a Orphans force a full cycle** (pre-mortem, revised after the panel). An upgraded machine with an unchanged
  policy gets 304 every cycle, so a recorded path not under the root of any enrolled, non-removed agent and not a
  state-dir file (an orphan) sends an empty etag. The full cycle then renders and prunes. Prune runs only in the full
  cycle, after the current roots are written; a 304 never deletes. No prune when a `Roots` override replaces the
  default root of an agent that has recorded files under that default root (the `--root` test flag). A file whose
  removal fails stays in `state.Files` for a retry. A renderer that stops emitting a file is pruned at the next
  policy change (a 304 renders nothing).
- **D3 Per-agent isolation = others proceed, cycle fails** (owner, 2026-10-26). Every per-agent error is isolated:
  unknown agent in machine.json (Lookup), no renderer, no root, unsafe rendered path, render error, file-write
  error. Agent X skips X's remaining files; other agents, the bundle and signature files are still written.
  X's previously recorded files (paths under X's root) are kept in `state.Files` and never pruned that cycle.
  `Result.Err` and `state.Error` name X; `aw-sync once` exits nonzero. The ETag is not saved on a partial cycle,
  so the next cycle refetches and retries X instead of getting 304. A failure writing a state-dir file (bundle,
  signature, trust) still fails the whole cycle as today. `state.Version` records the new revision; `state.Error`
  says which agents are still on an older one (`aw-sync status` already prints Error). Shapes considered: all-or-nothing (today; owner rejected).
- **D4 `aw-sync enroll --force` keeps the previous `state.Files`**, so the first cycle after a re-enroll that
  drops an agent can prune it (D2). Today enroll writes an empty state and those files are forgotten.
  An unreadable or corrupt state.json carries nothing (enroll prints a line saying files from before are not
  tracked). A re-enroll against another server or machine ID still prunes: the files are this machine's.
- **D5 Shared helper** `agent.DroppedRepoRules(bundle *policy.Bundle, agentName, file string) []string` replaces
  the codex and agy `dropped` copies; note text unchanged byte-for-byte.
- **D6 Docs**: README, docs/site, deploy/aw-sync/README, examples, web/content stop listing Gemini as supported.
  One "Gemini CLI" entry stays in docs/site/reference.md (or removing.md): removed, why, migrate to agy, what
  happens to existing rules and enrollments (D1, D2). No version number in messages (next release unknown).
- **D7 Small agy backlog items folded in**: README says `aw agy` is a thin launcher; agy Inspect test matches a
  specific Error message; header-value secret check uses TrimSpace and also catches `Token`/`Digest` schemes.

## Design

`internal/sync/sync.go` `Run`: render loop collects per-agent plans; a failing agent is recorded in a
`failed` set instead of returning. Write loop groups by agent; a write error marks that agent failed and skips
its remaining files. After writes, prune runs over `previous.Files − planned − kept(failed roots) − stateDir files`.
New state: `Files` = written ∪ kept; `ETag` empty if any agent failed; `Error` names failed agents.
Removed agents in machine.Agents: note and skip before `Registry.Lookup`. Agent root for a kept-file check uses
`cfg.root(name)`; a removed agent has no root, so its files are prune candidates.

## Acceptance criteria

1. Gemini code gone: `test ! -e internal/agent/gemini && ! git grep -n -i gemini -- '*.go' ':!*_test.go' ':!internal/policy/removed.go'` exits 0.
2. `awd apply` rejects a rule with a `gemini` key; error names the rule and `agy`: `go test ./internal/policy -run '^TestValidateRejectsRemovedAgent$' -v` PASS.
3. `aw-sync enroll --agents gemini` refused with the hint: `go test ./cmd/aw-sync -run '^TestEnrollRejectsRemovedAgent$' -v` PASS.
4. `aw gemini` exits nonzero, message contains `aw agy`: `go test ./cmd/aw -run '^TestLaunchRemovedAgent$' -v` PASS.
5. Machine enrolled `claude,gemini` with recorded unchanged Gemini files: cycle succeeds, claude files written, note names gemini, both Gemini files and any sidecar removed, state.Files has no Gemini path: `go test ./internal/sync -run '^TestRunSkipsRemovedAgentAndPrunes$' -v` PASS.
6a. Orphans force a full cycle: machine enrolled `claude,gemini`, policy unchanged, unchanged Gemini files removed by a full cycle, then a 304 once none is left; a root change and back never loses the default root files (`TestRunRootChangeNeverLosesAnAgentsFiles`): `go test ./internal/sync -run '^TestRunPruneOn304$' -v` PASS.
6. Prune: dropped agent's unchanged file + sidecar removed; edited file, symlink and missing file handled per D2 (kept+note, kept+note, silently untracked); non-absolute/unclean candidate skipped; state-dir files never removed; sidecar not removed when main file kept; nothing outside candidates touched: `go test ./internal/sync -run '^TestRunPrune' -v` lists ≥ 4 PASS and no FAIL.
7. `enroll --force` keeps previous state.Files; the next once prunes the dropped agent's file (e2e, real binaries): `go test ./cmd/aw-sync -run '^TestReenrollDropsAgentFiles$' -v` PASS.
8. Isolation: render failure for one of two agents and a write failure for one of two agents and an unknown agent in machine.json each leave the other agent written, the failed agent's old files on disk and in state.Files, Err naming the agent, ETag empty; the next cycle retries the agent: `go test ./internal/sync -run '^TestRunIsolat' -v` lists ≥ 4 PASS and no FAIL.
9. Shared helper: `! grep -n 'func dropped' internal/agent/*/render.go && go test ./internal/agent/... -run 'DroppedRepoRules|Golden|Render' -v` exits 0, codex and agy note text unchanged.
10. Whole suite: `go vet ./... && go test ./...` exits 0.
11. Docs: `for f in README.md docs/site/*.md deploy/aw-sync/README.md; do bash scripts/check-links.sh "$f" || exit 1; done && bash scripts/check-readme.sh` exits 0, and `grep -rn -i gemini README.md docs/site deploy examples web/content` prints only the removal entry (D6) and agy-successor mentions. check: seat panel-cdo for wording of the removal entry and migration hint.
12. Prune and isolation cannot remove a file aw-sync did not record or that changed since: check: seat panel-security.

## Security notes

aw-sync runs as root and now deletes files. It deletes only paths it recorded in root-owned state.json, only
regular files (Lstat, no symlink follow), only when content hash matches what it wrote. A changed file is
evidence of local edit or tamper and is left in place with a note. Isolation must not let a failed agent's
policy silently go stale: the cycle exits nonzero and refetches next cycle.

## Status (converged 2026-10-26)

Branch `adapter-cleanup` at b852afd against `main` (8567299). No earlier status table existed.

| # | criterion (short) | verdict | evidence |
|---|---|---|---|
| 1 | Gemini code gone | VERIFIED | AC1 command, exit 0 |
| 2 | `awd apply` rejects `gemini` rule, names agy | VERIFIED | `go test ./internal/policy -run '^TestValidateRejectsRemovedAgent$' -v`: PASS |
| 3 | `enroll --agents gemini` refused | VERIFIED | `go test ./cmd/aw-sync -run '^TestEnrollRejectsRemovedAgent$' -v`: PASS |
| 4 | `aw gemini` nonzero, mentions `aw agy` | VERIFIED | `go test ./cmd/aw -run '^TestLaunchRemovedAgent$' -v`: PASS |
| 5 | Removed agent skipped, files pruned | VERIFIED | `go test ./internal/sync -run '^TestRunSkipsRemovedAgentAndPrunes$' -v`: PASS |
| 6a | Orphans force full cycle; 304 after | VERIFIED | `go test ./internal/sync -run '^TestRunPruneOn304$' -v`: PASS |
| 6 | Prune rules (D2) | VERIFIED | `go test ./internal/sync -run '^TestRunPrune' -v`: 7 PASS, 0 FAIL |
| 7 | `enroll --force` keeps Files; next once prunes (e2e) | VERIFIED | `go test ./cmd/aw-sync -run '^TestReenrollDropsAgentFiles$' -v`: PASS |
| 8 | Per-agent isolation | VERIFIED | `go test ./internal/sync -run '^TestRunIsolat' -v`: 4 PASS, 0 FAIL |
| 9 | Shared `DroppedRepoRules` helper | VERIFIED | no `func dropped` in render.go; `go test ./internal/agent/... -run 'DroppedRepoRules\|Golden\|Render' -v`: all PASS, exit 0 |
| 10 | Whole suite | VERIFIED | `go vet ./... && go test ./...`: exit 0, no FAIL |
| 11 | Docs links/readme/grep; cdo wording | INCONCLUSIVE | owner checks at merge gate. Ran link and readme checks: pass. Ledger: `SEAT panel-cdo fail 4633631 AC11` (re-enroll command in sync note not runnable); owner decision: AC11 owner at gate; no `SEAT owner pass` line |
| 12 | Prune/isolation cannot remove unrecorded or changed file | INCONCLUSIVE | owner checks at merge gate. Ledger: `SEAT panel-security pass 4633631 AC12`; later commits 158698c, ac7a1d0 touched sync-adjacent files only via wave 2 and reviewer-lite found "AC12 regression none", but the security seat re-run cap is used and no `SEAT owner pass` line exists |

Deviations and notes:
- AC11 grep: besides the removal entry (docs/site/reference.md, removing.md) and agy-successor mentions (README.md:17, how-it-works.md:69), `docs/site/write-first-policy.md:98-101` lists `gemini-3.8-*` model IDs in an agy sample. These are agy model names, not Gemini CLI support claims; owner to confirm at the gate.
- AC12: the caveat from the seat (`--root` override prunes an enrolled agent's real files on 304) was narrowed in fix wave 1 (F1) and re-run as pass at 4633631.
- Open findings: none beyond AC11 (cdo re-enroll wording, owner at gate) and the backlog items named in TRIAGE and the wave-2 COORD line (docs URL).
