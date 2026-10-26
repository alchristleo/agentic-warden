# Fix wave (milestone panel, 2026-10-26), base 2dd5a40

Code (internal/sync/sync.go unless noted):
F1 [must, correctness+security] 304 prune deletes an enrolled agent's real files when its root changed (`--root` override, or a future SystemDir change); nothing re-renders them. Fix: delete the 304 prune branch. Before the fetch, if state.Files holds an orphan (not under StateDir, not under the root of any enrolled non-removed agent), send an empty etag so the cycle renders fully; prune runs only in the full cycle, after the current roots are written. Also: no prune at all when cfg.Roots has any override (test flag); one note says so. Test: enroll claude, run; run with Roots{"claude": other}; run normal cycles x2; assert default claude root holds the files after the last run and nothing under it was removed. Update TestRunPruneOn304 to the new shape (orphans force a full cycle; Gemini files still removed on an unchanged policy).
F2 [should, correctness] remove error (EBUSY/EPERM) drops the path from state.Files, never retried. Keep failed-remove candidates in state.Files, note it. Test.
F3 [must, design+correctness] prune note "agent no longer synced" wrong for a file a renderer stopped emitting → "removed X: aw-sync no longer manages it".
F4 [should, design] one keep-predicate helper (rootless → keep all + note), one prune call site, Files = written ∪ (prev − candidates) ∪ kept; replace failed map+failedOrder with sorted names or a small type. No behavior change beyond F1/F2.
F5 [should, design] stale comments: Result.Err ("Files on disk are untouched"), plannedFile ("held until every renderer has succeeded", removal "fails the cycle"). Rewrite for isolation (agent errors isolated; state-dir errors abort).
F6 [nit, correctness] error text "sync: agent x: sync: rendering x" → single "sync:" prefix.
F7 [must, design+cdo] launch message duplicates agy: hint becomes "Gemini CLI is no longer supported by agent-wrapper; use agy (Antigravity CLI): aw agy"; drop the "; run it with aw agy" suffix in cmd/aw/main.go. AC4 needs "aw agy" in the message.
F8 [nit, design] internal/policy/removed.go: unexported map + `func RemovedHint(name string) (string, bool)`; all callers use it. AC1 grep still allows only removed.go.
F9 [should, cdo + nit design] sync note for an enrolled removed agent adds the next step: "<name>: <hint>; re-enroll with `aw-sync enroll --force --agents ...` leaving <name> out (docs/site/reference.md#gemini-cli-removed)". awd apply and enroll errors end with the same docs pointer.
F10 [nit, correctness] cmd/aw doctor on a removed agent shows the RemovedHint, as launch does.
F11 [nit, design] cmd/aw/main.go:13-14 orphan sentence "doctor shows exactly what would run." and :254 "such as a policy file" reword.

Docs/site:
F12 [should, cdo] docs/assets/architecture.json + both SVGs: agy is not "rendered at launch, advisory". Advisory node = "Codex"; add an agy node/sublabel "agy: machine-wide admin file, gateway and models only" fed by aw-sync, both themes consistent.
F13 [should, cdo] web: one story. Add "Antigravity CLI (gateway and models)" to the web agent list (web/content/home.ts eyebrow and agents line :46, site/pricing where agents are listed); OG card and seo meta say "Antigravity CLI" not bare "agy". Hero "every coding agent" → "your coding agents".
F14 [should, cdo] docs/site/reference.md removal entry: add "If you still run Gemini CLI under a paid licence, aw no longer governs it; keep an older agent-wrapper release or move to agy."
F15 [should, cdo] reference.md:163 sample error: wrap across lines in a `text` block so the action part is visible at 1280px; must still match F7/F9 wording.
F16 [nit, cdo] removing.md:40 row → "Gemini (no longer written; aw-sync removes these if unchanged)".
F17 [must, cdo+design] README.md:17 and how-it-works.md:69: cut the retirement history to one clause on agy as Gemini CLI's successor; the news link and paid-licence detail live in reference.md.

Checks: go vet ./... && go test ./...; AC1-AC11 commands from the spec; cd web && pnpm test; check-links on README, docs/site/*.md, deploy/aw-sync/README.md; check-readme.sh.
Spec edits: D2a text → "orphans force a full cycle; prune runs only in the full cycle; no prune with --root overrides"; AC6a test name if renamed.
