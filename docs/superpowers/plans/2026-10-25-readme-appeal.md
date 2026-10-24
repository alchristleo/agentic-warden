# Plan: README appeal and colored terminal

Spec: `docs/superpowers/specs/2026-10-25-readme-appeal-design.md` (approved 2026-10-25).
Branch: `readme-appeal`, base `a1bf091`. Size: large, no trust boundary.

Parallel group A: T1, T2, T4, T5 (no shared files). Then T3 (needs T1 verdict and T2 colors).
Then T6 (owner records the two Claude tapes). Then panel (step 7).

Fixed shared names (so group A does not collide):
- Diagram files: `docs/assets/architecture-light.svg`, `docs/assets/architecture-dark.svg`,
  source `docs/assets/architecture.json`. T5 references them; T4 produces them.
- Hero: `docs/assets/hero.gif` (path unchanged).

## T1. Spike: command highlighting inside vhs (settles spec decision 9)

- agent: `sdd-implementer`. depends-on: none.
- Files: `docs/tapes/spike/` only (throwaway, deleted before merge), plus a verdict section
  appended to this plan under "T1 verdict".
- Try (a) ble.sh loaded by the tape's bash (pinned release tarball or commit, installed under
  the scratch dir / a temp HOME, never the real HOME); (b) `Set Shell zsh` +
  zsh-syntax-highlighting (pinned). For each: render `01-build`-like tape with vhs v0.12.1
  (ttyd 1.7.7 at `/tmp/claude-1000/-home-leo-AI-STUFF-agent-wrapper/c8ecab07-b5bc-4795-a80b-8af4e8b0ab6b/scratchpad/bin`),
  theme `Catppuccin Mocha`.
- Measure and record: does highlighting show in the GIF (extract a frame with ffmpeg, describe
  colors), render time vs current, any garbage chars or prompt glitches, whether the quickstart
  commands behave identically (zsh: `export X=$(...)`, `&`, `$PWD/bin:$PATH`), what CI install
  needs (apt package names, pinned URL), GIF size.
- Verdict: adopt (a), adopt (b), or neither, with evidence and the exact install lines for
  `lib/prep.sh` and `.github/workflows/tapes.yml`.

## T2. CLI color (`internal/termcolor` + call sites)

- agent: `sdd-implementer`. depends-on: none.
- Files: `internal/termcolor/termcolor.go`, `internal/termcolor/termcolor_test.go`,
  `cmd/aw/main.go`, `cmd/awd/main.go`, `cmd/aw-sync/main.go` and their tests.
- Interface (spec decision 7):
  ```go
  func Enabled(f *os.File) bool // char device && NO_COLOR empty && TERM != "dumb"
  func Green(on bool, s string) string // also Red, Yellow, Bold, Dim; on=false returns s unchanged
  ```
- Call sites and palette: exactly spec decision 6 and Design "CLI color" (quote: "Not in:
  `aw-policy`, `awd enroll-token` stdout, any JSON output, tabwriter tables"). Commands that print
  through an `io.Writer` (aw-sync) get `on` from the caller (stdout `*os.File` check in `main`), so
  tests pass `on` explicitly.
- Tests: `TestEnabled` (pipe file → false; NO_COLOR=1 → false; TERM=dumb → false);
  `TestWrappers` (off returns input byte-identical; on wraps with SGR and reset);
  doctor print test with color on: `ok:` green, `warn:` yellow, `error:` red (AC5);
  with color off: no `\x1b` (AC5); `EnrollTokenNoColor`: token on stdout has no `\x1b` (AC6).
  Existing aw-policy tests untouched and green (AC6).
- Constraint: no new dependency; do not change any text, only wrap.

## T3. Tapes look (depends on T1, T2)

- agent: `sdd-implementer`. depends-on: T1, T2.
- Files: all `docs/tapes/*.tape`, `docs/tapes/manual/*.tape`, `docs/tapes/lib/prep.sh`,
  optional `docs/tapes/lib/style.tape`, `.github/workflows/tapes.yml`, `docs/tapes/README.md`,
  re-rendered `docs/assets/01-05, 07` GIFs.
- Spec decision 8 (quote): "`Catppuccin Mocha` ... `Set WindowBar Colorful`, `Set BorderRadius 8`,
  `Set Margin 20` ... Prompt `\W $` with the directory in blue and `$` in green, set in
  `lib/prep.sh`." Highlighting per T1 verdict, pinned in CI. `vhs validate` on all tapes (AC7).
- GIF ≤ 1 MB each (AC9). Remove `docs/tapes/spike/`.
- Check: render all 6 CI tapes locally, extract one frame per GIF to the scratch dir for the gate.
- Run every command the `tapes` and `docs` CI jobs run (retro lesson), not just vhs.

## T4. Architecture diagram (archify)

- agent: `sdd-implementer-lite`. depends-on: none.
- Use the `archify` skill (`~/.claude/skills/archify/SKILL.md`). Content (spec decision 10, quote):
  "policy file → `awd` → `aw-sync` on each laptop → Claude Code / Codex / Gemini CLI, model traffic
  going straight to the vendor, not through `awd`". Ground node labels in `docs/site/how-it-works.md`.
- Outputs: the three fixed files above; each SVG ≤ 200 KB and readable at 900 px width on GitHub
  light and dark (AC10). If SVG renders badly in GitHub's image proxy (fonts), export PNG at 2x
  instead and use `.png` names, and tell the coordinator.

## T5. README rewrite + checker

- agent: `sdd-implementer`. depends-on: none (uses fixed diagram names).
- Files: `README.md`, `scripts/check-readme.sh`, `.github/workflows/docs.yml` (one step).
- Structure: spec Design "README (top to bottom)" items 1–11 exactly; H2 names per AC2:
  "The problem", "See it in 30 seconds", "How it works", "Why teams pick it", comparison H2,
  "Questions your security team will ask", "Try it". No demo link (decision 13).
- Use the `copywriting` skill for the copy; reader is a security/platform lead; no hype words.
- Every factual sentence must already be stated in `docs/site/*.md` or `docs/hosting/trust.md`;
  unsourced claims are dropped or softened (spec). Put a claim→source list in the task report.
- Try it per decision 12; `scripts/check-quickstart.sh README.md` must pass (AC4).
- `scripts/check-readme.sh`: AC1 + AC2 checks. Uses `gh api /markdown` when `gh` is authed, else
  curl with `GITHUB_TOKEN`; in docs.yml pass `GITHUB_TOKEN: ${{ github.token }}`.
- Tests: run it red on current `README.md` (the `<p>` + `# Agentic Warden` bug must fail it), green on the new one.

## T6. Manual Claude tapes (owner)

- depends-on: T3. Coordinator pings owner with exact commands from `docs/tapes/README.md`;
  owner renders `manual/06-claude.tape` and `manual/hero.tape`; coordinator checks frames for
  leaks (AC12), size (AC9), commits. Clean `~/.aw-try` before and after.

## Review (step 7)

- `panel-correctness`: whole branch + README claim audit (AC3).
- `panel-design` briefed as Chief Design Officer: uses project skills `frontend-design`,
  `web-design-guidelines`, `impeccable` (markdown references `critique.md`, `polish.md` only; never
  run its scripts), and `copywriting`; bar = archify README; inputs = README rendered on GitHub
  (branch pushed), hero + 2 step GIF frames, `/docs` index at 1280 and 390 px (AC11).
- Browser QA (Playwright) on `/docs` with new GIFs.
- Scoped re-run of each seat after the fix wave for AC3 and AC11.

## T1 verdict

**Adopted shape: (a) ble.sh in the tape's bash.** (b) not adopted: zsh is not installed locally and
needs sudo, so (b) is untested; nothing in (a) needs it.

Evidence (vhs v0.12.1, ttyd 1.7.7, theme Catppuccin Mocha, bash, ble.sh v0.4.0-devel3):
- Frame, last frame of the spike GIF: `/tmp/claude-1000/-home-leo-AI-STUFF-agent-wrapper/c8ecab07-b5bc-4795-a80b-8af4e8b0ab6b/scratchpad/a-last.png`.
  Mid-typing frame (vhs `Screenshot` while typing): `docs/tapes/spike/a-mid.png`.
- Colors seen: commands green (`git`, `go`, `ls`), builtins red-pink (`cd`, `export`), paths and
  URLs underlined blue, `PATH` bold magenta, variable name `X` red until the line is complete,
  `$(...)` default. Prompt dir blue, `$` green, from the `PS1` set before ble loads.
- Render time: spike tape (01-build-like, prep step 1 plus ble) 22 to 25 s; current `01-build`
  17.6 s (go build cached, clone from local checkout). GIF 75 KB versus 45 KB.
- Commands unchanged: the same bash line editor semantics; `export X=$(...)`, `&`, `$PWD/bin:$PATH`
  ran as in the plain tape (output `[1] pid`, `ls bin` listing).
- Glitches and fixes: (1) ble prints `[ble: EOF]` (prompt_eol_mark) after commands whose output
  does not end at column 0; fixed with `bleopt prompt_eol_mark=''`. (2) Loading ble inside the hidden
  prep with `--noattach` then `ble-attach` and `clear; echo` garbled the output and hung the Wait;
  use `--attach=prompt` and `Sleep 3s` before `clear`. (3) ble.sh must be sourced by absolute path
  (prep.sh cd's into a temp dir); use `$AW_REPO`. (4) Do not put `Wait+Line` on a ble prompt
  without testing; `Wait+Line@20s /\$\s*$/` worked in a probe. (5) first load generates a cache
  under the ble dir (`cache.d/`), slow once; CI start is always a first load, so the hidden
  prep absorbs it (wait for `prep done` plus `Sleep 3s`).

Install lines (pin the tarball by sha256, v0.4.0-devel3 tarball sha256
`c8612ee612bc6b10dbfd6e85c6cbdfd7caf152a12d1f9de22ea0a9d735b3080c`):
- `.github/workflows/tapes.yml` step (no apt needed; needs `xz`, preinstalled on ubuntu-latest):
  ```
  - name: Install ble.sh
    run: |
      curl -fsSL -o ble.tar.xz https://github.com/akinomyoga/ble.sh/releases/download/v0.4.0-devel3/ble-0.4.0-devel3.tar.xz
      echo "c8612ee612bc6b10dbfd6e85c6cbdfd7caf152a12d1f9de22ea0a9d735b3080c  ble.tar.xz" | sha256sum -c -
      mkdir -p "$RUNNER_TEMP/ble" && tar xJf ble.tar.xz -C "$RUNNER_TEMP/ble"
      echo "BLE_DIR=$RUNNER_TEMP/ble/ble-0.4.0-devel3" >> "$GITHUB_ENV"
  ```
- `lib/prep.sh`, after `PS1=...` is set (replace the plain PS1 with the colored one) and only
  when `BLE_DIR` is set (so local runs without ble still work):
  ```
  PS1='\[\e[34m\]\W\[\e[0m\] \[\e[32m\]\$\[\e[0m\] '
  [ -n "${BLE_DIR:-}" ] && { source "$BLE_DIR/ble.sh" --attach=prompt --norc; bleopt prompt_eol_mark=''; }
  ```
  The tapes then `Wait+Screen@120s /prep done/`, `Sleep 3s`, `Type "clear"`.
- Local dev: unpack the same tarball and `export BLE_DIR=...`; spike files in `docs/tapes/spike/`
  (`a-ble.tape`, `ble.sh`) are the working reference, delete before merge.

## Backlog

- Demo/contact link once a public site URL exists.
- Docs backlog from the last milestone (dedupe security-model vs threat-model; trust.md as a docs
  page; how-it-works detail to Reference; mobile tables breaking env var names).

- Design nits from scoped CDO re-run: hero policy comment dim (~3:1) and typed lines wrap mid-word; 07 shows mktemp `/tmp/tmp.*` paths and wraps; ble paints `$(` and `s` red in 05/07; light diagram edge-label pills off-white on tinted region, 9px subtitles.

## Ledger

PLAN a1bf091 written; group A next
T2 done 0290c9e sdd-implementer review approve (2 nits skipped)
T4 done 4bcfde8 sdd-implementer-lite fix1 coordinator: boundary label, legend, layout
T5 done 5096896 sdd-implementer fix2 (round1: 2 unsourced claims; round2: new unsourced line, restored known-gaps bullet)
MERGE T2 e6d86dd, T4 39cd79c, T5 merged; go test green; check-readme ok
T1 done 3378f50 sdd-implementer verdict: ble.sh (a); zsh untested (no sudo)
T3 done 6db2838 sdd-implementer review approve; coordinator 6b0d61e: one ble pin, if-block; 01 re-render ok
T6 next: owner renders manual/06-claude + manual/hero
T6 done cb51056 owner-approved coordinator render; frames checked, no leaks; 06 195KB hero 145KB
PANEL correctness fix-needed (AC3: 2 must, 3 should claims; win color; BLE_DIR masked failure); CDO fix-first (hero lacks policy, Claude coaching bypass, Codex/Gemini advisory mismatch [2 seats -> must], small GIFs, diagram contrast). Dismissed: CDO "05 never shows colored doctor" (bare aw doctor at 05-doctor.tape:43, frame verified); rename var in 05 (commands must match quickstart). Owner: demo prompt gets one-line rule.
FIX WAVE F1 README/quickstart, F2 diagram, F3 tapes, F4 Go
FIX WAVE merged: F4 f31fb45, F1 de8ece9, F2 4c3d3f4, F3 445bb50; manual GIFs re-rendered (hero 27s, 06 33s; Claude latency keeps hero >15s). Next: scoped re-run of correctness (AC3) + CDO (AC11), budget tier; then spec-converge.
PAUSED at step 7 (after fix wave, 5h usage 94%), next: scoped re-run panel-correctness (AC3 claims, model sonnet) + panel-design CDO (AC11) on branch 04b9407, then spec-converge, PR. Note AC6 test TestEnrollTokenNoColor deleted by F4 (no color path) -> update AC6 check text.
RESUME scoped re-run: correctness approve (8/8 fixed, AC3 all claims sourced; nit: tapes README `|| exit` fixed 1edd7c3; nit: re-add enroll-token test skipped, AC6 check now greps the no-color path f6f6adc). CDO approve, 0 must (AC11); should: What you get bullet 1 advisory caveat fixed 0cb7b40; 4 nits -> Backlog; dismissed again: 05 lacks doctor output (bare aw doctor at 05-doctor.tape:43). Next: spec-converge.
MERGED PR #4 b1c01cf (13/13 VERIFIED; AC7, AC12 owner-checked at gate)

## Retro (2026-10-25)

- **Numbers:** 6 tasks, 4 fix rounds (T4 1, T5 2, T3 1 coordinator), 0 blocked; panel 1 two-seat must (Codex/Gemini advisory) + 2 claim musts + CDO fix-first list; fix wave F1-F4; scoped re-runs 0 must, 1 should; 5h usage 94% at pause, 6% at end (after reset).
- **Cost:** panel-correctness was 19% of 24h usage at Opus/xhigh. Applied: Sonnet/medium default, Opus only on trust boundary, scoped re-run on fix diff only (lean-sdd CHANGELOG 2026-10-25).
- **Wasted:** CDO re-raised "05 lacks doctor output" after it was dismissed with evidence. Structure: scoped re-run briefs list dismissed findings with their reason.
- **Wasted:** spec-converge marked AC3/AC11 INCONCLUSIVE because seat verdicts lived only in a ledger line. Structure: save each seat report to `.git/panel-<seat>-<sha>.md` and pass the paths to spec-converge.
- **Kept:** ble.sh pinned by sha in `docs/tapes/lib/get-ble.sh`; tapes need `Sleep 600ms` after typing under ble.
