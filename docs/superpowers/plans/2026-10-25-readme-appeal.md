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

## Backlog

- Demo/contact link once a public site URL exists.
- Docs backlog from the last milestone (dedupe security-model vs threat-model; trust.md as a docs
  page; how-it-works detail to Reference; mobile tables breaking env var names).

## Ledger

PLAN a1bf091 written; group A next
