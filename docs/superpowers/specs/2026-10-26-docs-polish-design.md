# D: docs polish

## Goal

A security or platform engineer evaluating Agentic Warden, or a design partner about to sign, reads
each fact once, on the page that owns it, on desktop or phone. The README and docs media look
finished: no dim or wrapped hero text, no temp paths, no false red highlighting, readable diagram labels.

## Non-goals

- New pages, a docs restyle, quickstart rewrite.
- The README "Bring your rules" site link (needs a public site URL; Backlog).
- Removing `## Gemini CLI (removed)` from Reference (keep until the next release; Backlog).
- New claims. Every moved sentence keeps its meaning and its evidence.

## Decisions

1. **Split by reader.** `security-model.md` = short summary for an evaluator (holds / detects /
   does not stop / what the control plane sees). `threat-model.md` owns `## Design limits` with the
   full text and evidence, merged from both pages (security-model's evidence-rich bullets, plus
   threat-model's extra facts: NAT lockout up to a minute, syncs get 429 too, replays need a writer on
   the state dir or control plane host, remove a name to revoke). security-model keeps one sentence and a
   link to `./threat-model.md#design-limits`. Inbound `security-model.md#design-limits` links
   (README lines 89, 91) are repointed to threat-model. Shapes considered: merge into one page (rejected by owner),
   trim overlap only (rejected by owner).
2. **Trust statement source = `docs/site/trust.md`.** The full text of `docs/hosting/trust.md` replaces
   today's pointer page (front matter kept: section `security-review`, order 3). The pointer's
   evidence paragraph ("Where the statement makes a claim": `cell.tftest.hcl`, CloudTrail and deletion
   as operating practice, runbook) and its co-signing limit are kept, merged into the moved text.
   Every reference to the hosting file is repointed: README 88/93, threat-model 95/99 (bare
   "(trust.md)" becomes a link), runbook 302. `docs/hosting/trust.md` becomes a
   pointer of ≤ 5 lines to the docs page. Links in the moved text are rewritten relative to
   `docs/site/` (runbook stays at `docs/hosting/runbook.md`, linked by GitHub URL as today).
   Alternative considered: loader renders the hosting file (rejected by owner: loader code for one page).
3. **How it works = concepts.** Per-agent detail moves to Reference: the Codex `-c` launch-override
   paragraph (key charset rule) becomes `## Codex launch overrides`; the agy section's gateway API key
   and `aw doctor agy` wording become `## agy (Antigravity CLI)` beside `## agy managed keys`.
   How it works keeps its `## Antigravity CLI (gateway and models only)` heading (inbound anchor from
   README, Reference, security-model) with a 3–5 line summary (what is enforced, what is not) and a link
   there. Other content trimmed only where it repeats security-model or Reference.
4a. **Anchor check.** `scripts/check-links.sh` also resolves `file.md#anchor` and `#anchor` against
   the target's headings, with `renderMarkdown`'s slug rule (`web/lib/docs.ts`: lowercase, drop chars
   other than letters, digits, space, `_`, `-`; spaces to `-`; duplicates get `-1`). Fixture cases in
   `scripts/test-checks.sh`. Two shapes: extend check-links (chosen: one link checker, already in CI)
   or a new script (rejected: second parser of the same links).
4. **Mobile tables: code in table cells does not wrap inside a name.** CSS only:
   `.docs-prose td code, .docs-prose th code { overflow-wrap: normal; white-space: nowrap; }`.
   The table already scrolls (`display: block; overflow-x: auto`). Prose code keeps
   `overflow-wrap: anywhere` so the page itself never scrolls sideways.
5. **Media nits (owner: include).**
   - Hero (`manual/hero.tape`): policy comment colour ≥ 4.5:1 against the Catppuccin Mocha
     background `#1e1e2e`; no typed line wraps (shorter prompt, or the tape's width/font raised).
   - 07 (`07-reapply.tape`): no `/tmp/tmp.*` path on screen (hidden setup, or a fixed path), and no
     line wraps.
   - ble (`lib/prep.sh`): `$(` and the following word are not painted red in 05 and 07. Which faces
     paint them is an observable fact (pending: T5 first step runs `ble-face` on the pinned ble.sh from
     `get-ble.sh` and names them).
   - Diagram (`architecture-light.svg`, and `-dark` for parity): no 9 px text; edge-label pills
     contrast with the tinted region in light mode.
   - Re-render 05 and 07 locally with vhs v0.12.1 and ttyd 1.7.7 (apt's 1.6.3 is too old; the static
     build goes in the scratchpad, as in CI). Each re-rendered tape also writes a `.txt` frame dump
     (vhs `Output x.txt`, to the scratchpad, not committed) for the text checks. Hero and 06 need a
     logged-in Claude: see Gate question; default = owner records them by hand.

6. **aw's merged settings file ends with a newline** (added mid-build, owner 2026-10-26). `aw` writes
   `settings-<hash>.json` (`internal/agent/claude/claude.go:183`, `cache.Write`) without a final newline,
   so `cat "$settings"` (quickstart steps 6–7, tapes 05/07) leaves the next prompt on the `}` line. In the
   recordings ble's end-of-line probe hid this and left a stray character. Fix at the call site: the
   encoded JSON gets a trailing `\n`. The ble probe override (ef84d46) is reverted. The cache key is the
   content hash, so existing cache files are simply not reused; no migration.

## Design

One branch `docs-polish`, docs and CSS only, plus one Playwright test and tape/SVG edits. Tasks:
T1 security split, T2 trust move, T3 how-it-works → Reference, T4 mobile CSS + test,
T5 tapes + diagram + re-render, T6 anchor check (first: it turns stale anchors red for T1–T3).
T1–T3 share README cross-links, so they run in sequence after T6. T4 and T5 are independent.

## Acceptance criteria

1. Design limits live only in threat-model, and security-model keeps no limit's evidence. Check:
   `test "$(grep -c '^## Design limits' docs/site/security-model.md)" = 0 && test "$(grep -c '^## Design limits' docs/site/threat-model.md)" = 1 && ! grep -q 'TestServeTLSHTTPS\|TestLimiterRefills' docs/site/security-model.md`
2. No design-limit evidence or fact is lost: every test name from today's security-model Design limits,
   and the four threat-model-only facts, appear in threat-model. Check:
   `for t in TestFailsClosedWhenAKeyIsPinned TestFailsOpenWhenUnsigned TestExplicitRequireBundleFalseFailsOpen TestATamperedSignedBundleFailsClosedUnderRequireBundle TestServeTLSHTTPS TestCheckTransport TestEnrollRefusesAKeyIDMismatch TestLimiterBlocksAfterTenFailures TestLimiterRefills TestLimiterKeyWarnsOnceWithoutTrustedProxies TestAdminTokensActorIgnoresAppliedBy TestRunKeepsFilesWhenTheServerIsUnreachable 'up to a minute' 'get 429 too' 'control plane host' 'remove a name'; do grep -q "$t" docs/site/threat-model.md || { echo "missing $t"; exit 1; }; done`
3. security-model links to the limits. Check: `grep -q 'threat-model.md#design-limits' docs/site/security-model.md`
4. Every claim on the old security-model, threat-model, how-it-works, docs/site/trust and
   docs/hosting/trust pages has a home after the move, with the same meaning and evidence; no new claim.
   `check: seat panel-correctness`
5. The trust statement is the docs page: all 9 `##` headings of today's hosting file, the kept evidence
   paragraph, and a pointer file of ≤ 5 lines. Check:
   `for h in 'What we run for you' 'What we store' 'What never reaches us' 'Who holds the signing key' 'Where it runs' 'Access to your data' 'Backups and deletion' 'Subprocessors' 'Pilot terms'; do grep -q "^## $h" docs/site/trust.md || exit 1; done && grep -q 'cell.tftest.hcl' docs/site/trust.md && grep -q 'runbook' docs/site/trust.md && test "$(wc -l < docs/hosting/trust.md)" -le 5 && grep -q 'site/trust.md\|/docs/trust' docs/hosting/trust.md`
6. No reference to the hosting trust file or a bare "trust.md" outside a link remains. Check
   (allowlist: plans, specs, the pointer itself): `! git grep -n -E 'hosting/trust\.md|[(`]trust\.md[)`]' -- . ':!docs/superpowers' ':!docs/hosting/trust.md'`
7. How it works is concepts only: the agy section is a summary, and Reference carries the moved
   detail. Check:
   `test "$(awk '/^## Antigravity/{f=1;next} /^## /{f=0} f' docs/site/how-it-works.md | grep -c .)" -le 6 && ! grep -q 'AGY_LLM_GATEWAY_API_KEY\|only letters, digits' docs/site/how-it-works.md && grep -q '^## Codex launch overrides' docs/site/reference.md && grep -q 'AGY_LLM_GATEWAY_API_KEY' docs/site/reference.md && grep -q 'admin file present' docs/site/reference.md`
8. The link check resolves anchors: a broken anchor fixture fails, a good one passes. Check: `scripts/test-checks.sh`
9. All relative links and anchors resolve. Check:
   `scripts/check-links.sh README.md && for f in docs/site/*.md; do scripts/check-links.sh "$f"; done`
10. On a 390 px viewport, `/docs/reference` has no name broken across lines in a table cell (every
    `td code` and `th code` has one client rect; the test fails on today's CSS) and the page does not
    scroll sideways (`document.documentElement.scrollWidth ≤ 390`). Check:
    `cd web && pnpm exec playwright test e2e/docs.spec.ts -g "table code"`
11. The site still builds and its tests pass. Check:
    `cd web && pnpm typecheck && pnpm test && pnpm build && pnpm test:e2e`
12. No diagram text below 10 px, and the SVGs parse. Check:
    `! grep -ohE 'font-size="[0-9.]+"' docs/assets/architecture-light.svg docs/assets/architecture-dark.svg | grep -oE '[0-9.]+' | awk '$1<10{f=1} END{exit !f}' && scripts/check-svg.sh`
13. Tapes validate, and the re-rendered 07 shows no temp path. Check:
    `vhs validate docs/tapes/*.tape docs/tapes/manual/*.tape && ! grep -q '/tmp/tmp\.' "$SCRATCH/07-reapply.txt"`
14. Media nits fixed as Decision 5 states (hero comment ≥ 4.5:1 and no wrap, 07 no wrap, no red `$(`
    in 05/07, light diagram pills readable), judged from re-rendered GIF frames. `check: seat panel-cdo`
    (hero part: `check: owner at gate` if the owner records the hero by hand).
15. The security-review section reads well for the target reader on desktop and 390 px (one fact,
    one place; nav order security model → threat model → trust statement). `check: seat panel-cdo`

16. aw's merged settings file ends with exactly one newline. Check:
    `go test ./internal/agent/claude/ -run TestSettingsFileEndsWithNewline -v`
17. The re-rendered 05 and 07 show no stray line after `settings=$(…)` and the JSON's closing `}` on its
    own line before the prompt. `check: seat panel-cdo`

## Gate question

Hero and 06 need a logged-in Claude Code. Default: you record them by hand after the tape edits land
(`docs/tapes/README.md` has the commands). Alternative: an agent re-renders them in this session with
your login (two short `claude -p` runs each).
