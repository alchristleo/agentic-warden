# D: docs polish plan

Spec: `docs/superpowers/specs/2026-10-26-docs-polish-design.md` (approved 2026-10-26). Branch `docs-polish`.
Base: f70698d. Task numbers follow the spec (T6 runs first).

Order: **group A in parallel** (T6, T4, T5; no shared files), then T1 → T2 → T3 in sequence
(all three edit README cross-links and each other's pages).

Constraints for every docs task: no new claims; a moved sentence keeps its meaning and the file/test
it cites. Plain English, short sentences (asd-ste100 rules in the implementer agents). Run
`scripts/check-links.sh` on every touched `.md` and the spec ACs the task owns before reporting.

## T6. Anchor check in check-links.sh (group A)

- **Files:** `scripts/check-links.sh`, `scripts/test-checks.sh`, `scripts/testdata/` (new fixtures).
- **Interface:** unchanged CLI `check-links.sh <file.md>`. New: a link `x.md#frag` or `#frag` fails
  unless `frag` equals the slug of a heading in the target (`#frag` = same file). Slug rule copied from
  `web/lib/docs.ts` `renderMarkdown` (read it; lowercase, drop chars other than letters, digits, space,
  `_`, `-`; spaces to `-`; a repeated slug gets `-1`, `-2`). Headings inside fenced code are ignored.
  `/docs/<page>#frag` maps to `docs/site/<page>.md` as today. Anchors on non-`.md` targets and external
  URLs are not checked. `#top` is always valid (`web/lib/docs.test.ts` uses `index.md#top`).
- **Tests (in test-checks.sh, `expect` style already there):** `links: a broken anchor exits 1 and names
  it`; `links: a good anchor to another file passes`; `links: a same-page anchor passes`;
  `links: a duplicate heading's -1 anchor passes`; `links: #top passes`. Add a cross-check that the
  shell slug equals the TS slug for 3 real headings (e.g. `6. Ask Claude to run curl in both
  repositories` → `6-ask-claude-to-run-curl-in-both-repositories`).
- **Expected after T6:** `check-links.sh README.md` turns red only if an anchor is already stale on main;
  report which (do not fix docs pages here; T1–T3 own them).
- **ACs:** 8 (and enables 9). **Agent:** sdd-implementer. **Depends-on:** none.

## T4. Mobile table code (group A)

- **Files:** `web/app/docs/docs.css`, `web/e2e/docs.spec.ts`.
- **Change:** spec Decision 4 CSS. Prose code keeps `overflow-wrap: anywhere`.
- **Test:** in the existing `mobile` describe (390×844): `"table code stays on one line"` on
  `/docs/reference`: at least 5 `td code` elements exist; each `td code, th code` has
  `getClientRects().length === 1`; `document.documentElement.scrollWidth <= 390`. Run it red on the
  current CSS first and quote the failure in the report.
- **ACs:** 10, 11. **Agent:** sdd-implementer. **Depends-on:** none.

## T5. Tapes, diagram, re-render (group A)

- **Files:** `docs/tapes/lib/prep.sh`, `docs/tapes/07-reapply.tape`, `docs/tapes/05-doctor.tape` (only if
  needed), `docs/tapes/manual/hero.tape`, `docs/assets/architecture-{light,dark}.svg`,
  `docs/assets/architecture.json` (if it is the SVG source; read it), `docs/assets/05-doctor.gif`,
  `docs/assets/07-reapply.gif`, `docs/tapes/README.md` (only if commands change).
- **Step 1 (settles the pending fact):** source the pinned ble.sh (`docs/tapes/lib/get-ble.sh`) and find
  which faces paint `$(` and the next word red (`ble-face` listing; e.g. `syntax_error`,
  `command_*`, `syntax_expr`). Name them in the report. Set them in `prep.sh` beside line 81.
- **Hero:** policy comment colour ≥ 4.5:1 on `#1e1e2e` (state the hex and the ratio); no typed line
  wraps at the tape's width and font (shorten the prompt keeping its meaning, or widen; count columns).
  Do not render hero or 06 (they need the owner's Claude login).
- **07:** no `/tmp/tmp.*` on screen (hidden setup or a fixed readable path), no wrapped line.
- **Diagram:** every `font-size` ≥ 10 in both SVGs; light-mode edge-label pills visibly contrast with the
  tinted region (state fill hexes and ratio ≥ 3:1). Keep both themes in step.
- **Render:** vhs v0.12.1 (on PATH), ttyd 1.7.7 static build downloaded to
  `$SCRATCH=/tmp/claude-1000/-home-leo-AI-STUFF-agent-wrapper/180127ba-dbfc-4097-8606-f8e4e09b887c/scratchpad`
  and put first on PATH (apt ttyd 1.6.3 is too old). Re-render 05 and 07. For each, also render a text
  dump to `$SCRATCH/<name>.txt` (add a temporary `Output` line or a copy of the tape in the scratchpad;
  do not commit the txt). Extract 3 PNG frames per GIF to `$SCRATCH/frames/` for the CDO seat.
- **ACs:** 12, 13 (txt grep), 14 (seat). **Agent:** sdd-implementer (network download, vhs).
  **Depends-on:** none.

## T1. Security pages split (after group A)

- **Files:** `docs/site/security-model.md`, `docs/site/threat-model.md`, `README.md` (lines 89, 91 anchors).
- **Change:** spec Decision 1. threat-model `## Design limits` = merged full text: security-model's
  bullets with their evidence + threat-model-only facts (NAT lockout "up to a minute", syncs "get 429
  too", replays need a writer on the state directory or "control plane host", "remove a name" to revoke).
  security-model: delete its Design limits section; one sentence + link `./threat-model.md#design-limits`;
  keep holds / detects / does not stop / control plane sees. The "It is a client-side control" bullet that
  says "See the next section" must still point somewhere true.
- **Checks:** spec AC1, AC2, AC3, AC9 green.
- **Agent:** sdd-implementer-lite. **Depends-on:** T6.

## T2. Trust statement move

- **Files:** `docs/site/trust.md`, `docs/hosting/trust.md`, `README.md` (88, 93),
  `docs/site/threat-model.md` (95, 99), `docs/hosting/runbook.md` (302).
- **Change:** spec Decision 2. Rewrite links of the moved text for `docs/site/` (repo files outside
  docs/site by GitHub URL, as the site does elsewhere; docs pages as `./x.md`). Pointer file ≤ 5 lines,
  linking `../site/trust.md`.
- **Checks:** spec AC5, AC6, AC9 green.
- **Agent:** sdd-implementer-lite. **Depends-on:** T1.

## T3. How it works → Reference

- **Files:** `docs/site/how-it-works.md`, `docs/site/reference.md`.
- **Change:** spec Decision 3. New Reference sections `## Codex launch overrides` and
  `## agy (Antigravity CLI)` (next to `## agy managed keys`). how-it-works keeps the
  `## Antigravity CLI (gateway and models only)` heading with ≤ 6 non-blank lines and a link to
  `./reference.md#agy-antigravity-cli`. Trim other how-it-works text only where it repeats
  security-model or Reference; list every trimmed sentence and where it lives in the report.
- **Checks:** spec AC7, AC9 green.
- **Agent:** sdd-implementer-lite. **Depends-on:** T2.

## Backlog

- README "Bring your rules" link once a public site URL exists.
- Remove `## Gemini CLI (removed)` from Reference after the next release.
- Hero and 06 GIFs: owner re-records by hand after T5 (`docs/tapes/README.md`).
- Stale `.claude/worktrees/agent-*` directories from earlier milestones (8+): clean up.

- check-links anchor nits (T6 review): skip the page's first h1 (site strips it; -N drift if repeated); `_x_` emphasis slug; fixture for `/docs/<page>#frag`.

## Ledger

PLAN f70698d spec approved; plan written; group A (T6, T4, T5) next
USAGE start 21
T4 done a98944b sdd-implementer red docs.spec.ts:95 then green; typecheck, 84 unit, build, 48 e2e pass; first hand-back was 'pending', re-asked
T6 done fd9b6d0 sdd-implementer anchors resolve via renderMarkdown slug rule h1-h3; no stale anchors on main; SIGPIPE false-positive fixed
T4 review approve a98944b sdd-reviewer red re-verified by removing rule; 2 nits skipped (untouched css spacing, trailer info)
T6 review approve fd9b6d0 sdd-reviewer break-it exit 1 verified; 3 nits -> Backlog
T1 done 61db48a sdd-implementer-lite AC1-3 pass, links pass; 97 STE hits in moved text left as is (spec: no restyle)
T1 fix 61db48a sdd-reviewer-lite fix-needed should: lost '(no signer, or an older awd)'; nit: false renderer-failure claim (pre-existing)
T1 coord 10c0e14 restore no-key-id parenthetical
T1 coord b8e4474 owner-approved: failing renderer skips only its agent, cites TestRunWritesOtherAgentsWhenOneRendererFails
T5 blocked 7d56247 sdd-implementer edits done; GIF re-render blocked: classifier denied running downloaded ttyd 1.7.7
T1 fix b8e4474 sdd-reviewer-lite fix-needed should: agent.go Renderer comment repeats old claim
T1 coord 8cdf89a Renderer comment: failing renderer skips only its agent
T1 done 8cdf89a sdd-reviewer-lite approve coord fixes (Renderer comment matches sync.go)
T2 done 74bab2c sdd-implementer-lite AC5 AC6 pass, links pass; pointer 3 lines; commit body overclaims 'noted in plan'
T2 review approve 74bab2c sdd-reviewer-lite all sentences kept; nit commit body skipped
T3 done acbb1cf sdd-implementer-lite AC7 red->green, links pass; agy summary 4 lines
T3 review approve acbb1cf sdd-reviewer-lite all moved sentences kept; nit commit body skipped
