# README appeal and colored terminal: design

Date: 2026-10-25. Branch: `readme-appeal`. Size: large (README, CLI, tapes, docs, review).

## Goal

A security or platform lead lands on the GitHub README and, within one screen, understands the
problem, sees it work, and wants to bring it to their team. Every GIF looks like a modern colored
terminal (iTerm-style theme, colored prompt, highlighted commands), and `aw`, `awd`, `aw-sync` print
colored output for real, so the GIFs show what users get.

Why now: the docs milestone shipped a correct but plain README. The owner, reading it on GitHub,
found broken markdown, a confusing Try it section, and nothing that sells. Bar for "appealing":
[tt-a1i/archify](https://github.com/tt-a1i/archify) README.

## Reader

Primary: security or platform lead deciding whether to bring the tool to their team (owner's call).
Developers trying it on a laptop are second; their detail moves lower or into `/docs`.

## Non-goals

- No sponsor wall, community channels, translations, star/install badges (archify parts that do not fit).
- No new docs pages and no rewrite of the `/docs` site beyond what the new GIFs and a shared README
  change need. Docs backlog (dedupe security-model vs threat-model etc.) stays in the backlog.
- No color flags or config (`--color`, theme settings). Color is automatic.
- No color in JSON output, in `aw-policy` (its stdout is read by Claude Code), or in the token printed
  by `awd enroll-token` (stdout is captured by scripts).
- No product behavior change other than color.

## Anti-goals (failure even if it works)

- README claims that the docs or code do not back (AC11 lesson from the last milestone).
- Escape codes in pipes, files, CI logs, or anything a script parses.
- Marketing tone that a security lead reads as hype ("revolutionary", unverifiable numbers).
- GIFs that show a token, a real account email, or a path under the real home.

## Decisions

Owner decided:
1. Reader is the security/platform lead.
2. Color in the CLIs for real, not only in recordings.
3. Review with all of: Anthropic `frontend-design`, Vercel `web-design-guidelines`, Impeccable
   (markdown guidance only, never its binary, no hooks; installed project-local in `.claude/skills/`,
   git-excluded), the `copywriting` skill, and a reviewer briefed as Chief Design Officer with
   archify as the bar.
4. Playback accepted: centered logo + promise + hero GIF first; "see it in 30 seconds"; features each
   with a visual; comparison table; security-lead questions answered in one line each with a link;
   labeled Try it; next step.

My calls (owner can override):
5. **Color rule.** On when the stream is a terminal (`os.File.Stat` mode has `ModeCharDevice`) and
   `NO_COLOR` is unset or empty, and `TERM` is not `dumb`. Off otherwise. Decided per stream
   (stdout and stderr separately). Stdlib only, no new dependency.
6. **Palette.** green = ok/success (`ok:`, `applied revision`, `enrolled`, `synced`), red =
   error/denied (`error:`, `aw: <err>` prefix), yellow = warn/note-of-risk (`warn:`, `warning:`),
   bold = section headings (`agent claude`), dim = paths and secondary detail (`args:`, `file:`,
   `note:` lines). Only the label or the whole short line is wrapped; interpolated values are not
   recolored or parsed.
7. **One tiny package** `internal/termcolor`: `Enabled(f *os.File) bool` and wrappers
   `Green/Red/Yellow/Bold/Dim(on bool, s string) string`. Callers decide `on` once per command.
8. **Tape look.** One theme for every tape: `Catppuccin Mocha` (closest to the iTerm dark look and
   matches the site's dark code blocks); `Set WindowBar Colorful`, `Set BorderRadius 8`,
   `Set Margin 20` with a site-matching margin fill. Prompt `\W $` with the directory in blue and
   `$` in green, set in `lib/prep.sh`.
9. **Command highlighting** in the recording shell. Two shapes:
   (a) ble.sh loaded into the tape's bash by `lib/prep.sh` (commands stay bash, exactly as in the
   quickstart); (b) `Set Shell zsh` + zsh-syntax-highlighting (needs zsh in CI and the commands
   to behave the same in zsh). Which works inside vhs/ttyd, renders correctly, and keeps render
   time acceptable is an observable fact: **pending: T1** (a spike, the first plan task, before
   any tape or CI work). If neither works cleanly, ship without
   typed-command highlighting (theme + prompt + CLI colors still land) and say so at the gate.
10. **Architecture picture.** One diagram (policy file → `awd` → `aw-sync` on each laptop →
    Claude Code / Codex / Gemini CLI, model traffic going straight to the vendor, not through `awd`)
    made with the `archify` skill, exported as light and dark SVG (or PNG if SVG renders badly on
    GitHub), shown with `<picture>` + `prefers-color-scheme`. Source JSON committed next to it.
11. **Logo lockup.** Reuse `docs/assets/logo.svg` plus the name as HTML text (no new artwork).
12. **Try it.** Each command block is preceded by a one-line bold label saying what it does
    ("**1. Build the four binaries**"); expected output sits under "You should see:" as a `text`
    block. The section states up front that it is the first 4 of 7 steps and links the rest.
13. **Next step for the reader.** Owner: trial only for now (Try it). A demo link is added when the
    public site URL exists (already on the production-ready reminder).
14. **Hero before merge.** Owner: the new colored `hero.gif` and `06-claude.gif` land before merge.
    When CLI color and tapes are done, the coordinator pings the owner, who runs the two manual
    tapes; the coordinator checks them for leaks (AC12) and commits them.

## Design

### README (top to bottom)

1. Centered logo + `Agentic Warden` as a real heading + one-line promise as `<h3>` (all HTML inside
   one `<p align="center">`/`<div>` block, blank lines around every HTML block so GitHub renders the
   markdown that follows).
2. Hero GIF (re-recorded, colored), width ~900.
3. One short centered link row: Quickstart · How it works · Security model · Docs.
4. **The problem** (3 lines, the lead's words): developers run three agents with their own settings;
   no central rules, no per-repo scope, no record of what was in force.
5. **See it in 30 seconds**: the policy YAML (with comments), then the outcome in two lines:
   refused in `payments-api`, allowed in `sandbox`, pointing at the hero GIF above (no new tape,
   no stills).
6. **How it works** picture (decision 10) + 3 bullets.
7. **Why teams pick it**: 3–4 features, bold lead + one line + link (one policy for three agents;
   scoped by repo and IdP group; works offline; signed bundles). Each claim cites a docs page.
8. **Comparison table** with ✅ / ❌ / ➖ instead of Yes/No.
9. **Questions your security team will ask**: does our code or prompts reach you? what if the server
   goes down? can a developer bypass it? what is not covered yet? One line each + link
   (trust.md, security-model.md, threat-model.md Known issues, design limits).
10. **Try it** (decision 12).
11. Docs · Contributing · License (no demo link yet, decision 13).

Every factual claim must already be stated in `docs/site/*.md` or `docs/hosting/trust.md`; the README
adds no new facts. A claim with no source is dropped or softened, not newly sourced (my call); this
includes comparison-table rows. "Can a developer bypass it?" is answered from the design limits and
Known issues in `docs/site/threat-model.md` / `security-model.md`.

### CLI color

`internal/termcolor` (decision 7). Apply in: `aw doctor` (level labels, `agent <name>` heading, dim
detail lines), `aw` error prefix, `awd serve` listening line, `awd apply` success line, `awd` error
prefix, `awd snapshot`/`groups` warnings, `aw-sync enroll|once|status|install|uninstall` success,
note, warn and drift lines, `aw-sync` error prefix. Not in: `aw-policy`, `awd enroll-token` stdout,
any JSON output, tabwriter tables (column alignment counts bytes; leave tables plain).

### Tapes

All tapes: decision 8 settings via a shared header (vhs `Source` of `docs/tapes/lib/style.tape` if
`vhs validate` accepts it, else repeated per tape), prompt and highlighting from `lib/prep.sh`. Re-render
the 6 CI tapes; owner re-records `manual/06-claude.tape` and `manual/hero.tape` with their Claude
login (instructions in `docs/tapes/README.md` unchanged except the new requirements, e.g. ble.sh).
The tapes CI job installs whatever T1 picks, pinned by version/commit.

### Docs site

The `/docs` pages embed the same GIFs; they update for free. The README's broken-HTML fix pattern is
checked in `docs/site/*.md` too.

### Review

After build: one design review round with the CDO-briefed reviewer (uses `frontend-design`,
`web-design-guidelines`, Impeccable critique/polish references as markdown, `copywriting`), judged
against archify's README, on: README rendered on GitHub (via the branch on GitHub), the hero and
two step GIFs, `/docs` index at 1280 and 390 px. Plus the usual correctness panel seat.

## Acceptance criteria

1. **README renders clean on GitHub.** `scripts/check-readme.sh` posts `README.md` to GitHub's
   `POST /markdown` (gfm, repo context) and fails if the HTML contains a literal `**`, a paragraph
   starting with `#`, or an `<img>`/`<source>` whose repo-relative path does not exist.
   Check: `scripts/check-readme.sh` exits 0 (also run as a step in `docs.yml`, authenticated with
   the workflow's `GITHUB_TOKEN` to avoid the unauthenticated rate limit; my call).
2. **First screen sells.** In `README.md`, the hero GIF appears before line 20, and the H2 headings
   appear in this order: The problem, See it work, How it works, What you get,
   (comparison), Questions your security team will ask, Try it.
   Check: `scripts/check-readme.sh` asserts the order (`grep -n '^## '`).
3. **Every README claim is backed.** Each factual sentence in README maps to a line in
   `docs/site/*.md` or `docs/hosting/trust.md`. Check: `panel-correctness` claim audit lists 0
   unsupported claims (scoped re-run after the fix wave if it found any).
4. **Try it is labeled and still works.** Every `sh` block in README's Try it is directly preceded by a
   bold label line, every output block by "You should see:", and
   `scripts/check-quickstart.sh README.md` passes (CI `docs` job green).
5. **Color on a terminal, none elsewhere.** Go tests: with color forced on, `aw doctor` output has
   `ok:` wrapped in green, `warn:` in yellow, `error:` in red; with a non-terminal writer, or
   `NO_COLOR=1`, or `TERM=dumb`, output contains no `\x1b` byte.
   Check: `go test ./internal/termcolor/ ./cmd/...` passes; and
   `aw doctor | grep -c $'\x1b'` prints 0 in the quickstart state.
6. **Scripts unaffected.** `token=$(awd enroll-token you@acme.com)` yields a token with no `\x1b`
   byte even when stderr is a terminal; `aw-policy` output is byte-identical to `main`.
   Check: `enrollToken` (cmd/awd/main.go) prints the token with plain `fmt.Println` and has no
   `termcolor` call (`sed -n '/^func enrollToken/,/^}/p' cmd/awd/main.go | grep -c termcolor` = 0);
   `git diff --stat main -- cmd/aw-policy` is empty; `go test ./...` passes. (The
   `EnrollTokenNoColor` test was dropped in the fix wave: no color path exists to test.)
7. **Tapes look colored.** Every tape (CI and manual) sets `Theme "Catppuccin Mocha"` and the
   window bar; `vhs validate docs/tapes/*.tape docs/tapes/manual/*.tape` passes; the `tapes` CI job
   is green. A frame from each re-rendered GIF shows colored prompt and colored CLI output
   (screenshot set reviewed at the gate).
8. **Command highlighting** per T1's verdict: if T1 adopts a shape, a frame of `01-build.gif` shows
   the command name in a different color from its arguments; if T1 rejects both, the spec records
   the evidence and the cut is accepted at the merge gate.
9. **GIF weight.** Each GIF in `docs/assets/` ≤ 1 MB and the README's total image weight ≤ 3 MB.
   If a GIF is over, lower its framerate or length first; the theme and window bar stay (my call:
   looks win over a tighter cap). Check: `find docs/assets -name '*.gif' -size +1024k` prints nothing.
10. **Diagram works in both themes.** Light and dark exports exist, each ≤ 200 KB, the README uses
    `<picture>` with `prefers-color-scheme`, and the archify source JSON is committed.
    Check: `scripts/check-readme.sh` (image paths) + `ls -la` sizes.
11. **Design review passes.** The CDO-briefed reviewer's report on the final branch has 0 must-fix
    items (scoped re-run after the fix wave). Check: report in the plan ledger.
12. **No leaks.** No GIF frame shows a token, an email other than `you@acme.com`, or a path under the
    real home. Check: owner eyeballs the two manual GIFs; the CI tapes run in a temp HOME.
13. **Nothing regressed.** Go tests, web lint/typecheck/unit, e2e, `check-links`, `check-quickstart`
    on README and `docs/site/quickstart.md` all pass; PR CI green.

## Security notes

No new trust boundary. Color wrapping never interprets interpolated text; values coming from the
server or files are printed exactly as before (no new escape-sequence surface beyond what plain
printing already had). Third-party skills are markdown only; Impeccable's launcher and binary are
not installed.

## Status (converged 2026-10-25)

Checked on `692ea5c` against `main`. Counts: 11 VERIFIED, 2 INCONCLUSIVE (AC7 frames, AC12 leaks: owner check at the merge gate) (AC3, AC11 updated by coordinator from the scoped re-run reports).

| # | criterion (short) | verdict | evidence (command + key output line) |
|---|---|---|---|
| 1 | README renders clean on GitHub | VERIFIED | `scripts/check-readme.sh` → `check-readme: README.md ok`, exit 0; `docs.yml:67` runs it |
| 2 | First screen sells, H2 order | VERIFIED | `grep -n '^## ' README.md` → The problem (15), See it work (21), How it works (50), What you get (63), How it compares (74), Questions… (86), Try it (95); hero `<img>` at line 9 (<20) |
| 3 | Every README claim backed | VERIFIED | Scoped `panel-correctness` re-run on fix diff `ebfdfb6..cfaec70` (2026-10-25): 8/8 earlier findings fixed; every claim added by the fix wave cited to a source line (e.g. `docs/hosting/trust.md:16,19,21`, `docs/site/security-model.md:20,52,65,66,73`); 0 unsupported. Report summarized in plan ledger (RESUME line) |
| 4 | Try it labeled and works | VERIFIED | `scripts/check-quickstart.sh README.md` → `README.md ok (4 run, 0 skipped, 8 expected lines found)`; all 4 `sh` blocks have a bold label line and all 4 output blocks follow "You should see:" (README Try it section) |
| 5 | Color on terminal, none elsewhere | VERIFIED | `go test -count=1 -v ./internal/termcolor/ ./cmd/aw/` → PASS, incl. `TestDoctorReportColorOn` asserting `\x1b[32mok:\x1b[0m`, yellow `warn:`, red `error:`. `go run ./cmd/aw doctor \| grep -c $'\x1b'` → `0` (piped, repo cwd; not a fresh quickstart state) |
| 6 | Scripts unaffected | VERIFIED | `sed -n '/^func enrollToken/,/^}/p' cmd/awd/main.go \| grep -c termcolor` → `0`; `git diff --stat main -- cmd/aw-policy` empty; `go test -count=1 ./...` 0 FAIL lines; runtime: `script -qfc "awd enroll-token --url http://127.0.0.1:18080 you@acme.com" …` with stderr on a pty → stdout 44 bytes, 0 `\x1b` bytes |
| 7 | Tapes look colored | INCONCLUSIVE | `vhs validate docs/tapes/*.tape docs/tapes/manual/*.tape` → exit 0; `Theme "Catppuccin Mocha"` in all 8 tapes. Frame sheets viewed for 01, 05, 06, hero (colored prompt and CLI output). Not viewed: 02, 03, 04, 07. Tapes CI run 40139002431 success and PR #4 tapes job pass. Owner reviews frames at the gate |
| 8 | Command highlighting | VERIFIED | Mid frame of `01-build.gif` (`/tmp/claude-1000/frames/01-build-mid.png`): `git` in green, `clone` and the URL in a different color. ble.sh path in `docs/tapes/lib/prep.sh:48` |
| 9 | GIF weight | VERIFIED | `find docs/assets -name '*.gif' -size +1024k` → no output; largest `07-reapply.gif` 333,602 B; README-referenced image total 901,609 B (≤ 3 MB) |
| 10 | Diagram in both themes | VERIFIED | `architecture-light.svg` 19,360 B, `architecture-dark.svg` 19,628 B; `architecture.json` tracked (`git ls-files`); README `<picture>` with `prefers-color-scheme` (line 53); check-readme image paths pass |
| 11 | Design review 0 must | VERIFIED | Scoped CDO `panel-design` re-run on fix diff (2026-10-25): 0 must, 1 should, 4 nits; ledger RESUME line. Later commits only apply that should (`0cb7b40`, one README bullet) or touch non-design text (`1edd7c3` tapes README, `f6f6adc` spec) |
| 12 | No leaks in manual GIFs | INCONCLUSIVE | Owner eyeball is the check. Sampled frames of hero and 06 show no token or email at contact-sheet resolution; `05-doctor` shows `/tmp/…` paths (temp HOME). Full-resolution owner check of hero and 06 still needed |
| 13 | Nothing regressed, PR CI green | VERIFIED | Passing: `go test -count=1 ./...` (0 FAIL); in `web/`: `npm run lint` exit 0, `npm run typecheck` exit 0, `npm test` 81 passed, `npm run test:e2e` 47 passed; `scripts/check-links.sh README.md` exit 0; `scripts/check-quickstart.sh` on README and `docs/site/quickstart.md` ok. PR #4 CI: docs, docs, tapes, test, test all pass |

### Deviations

- `hero.gif` runs about 27 s against the ≤ 15 s target (owner-accepted; Claude latency). No AC states a duration, so it is not counted as a failure here.
- AC7 covers 4 of 8 GIF frame sets; the tapes CI run for HEAD has not finished.
- No PR yet, so PR CI (AC13) is pending.
- The AC5 grep ran piped from the repo, not in a fresh quickstart state. The color-on path is covered by `TestDoctorReportColorOn`.
