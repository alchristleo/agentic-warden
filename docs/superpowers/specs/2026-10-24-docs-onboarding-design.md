# Docs and onboarding: design

Date: 2026-10-24. Branch: `docs-onboarding`. Size: large (lean-sdd full flow).
Inputs: exec-room papers in `docs/superpowers/exec-room/2026-10-24/` (CEO, CPO, CMO, two rounds),
shared context in `.agents/product-marketing.md`.

## Goal

A platform or security lead who lands on the README or `/docs` understands in 60 seconds what
Agentic Warden does, sees their own rule stop their own Claude Code in about 10 minutes without root,
and finds the security model and its limits before they talk to us.

## Non-goals (cut or deferred)

- Release binaries, `install.sh`, Go module path rename: next milestone (user decision 2026-10-24).
  The quickstart builds from source with `go build`.
- Pricing changes, named-vendor comparison page, search-entry pages, quickstart completion signal:
  Backlog.
- Docs framework (Nextra, Fumadocs, MDX): not needed for about 10 pages.
- Guides and Reference pages beyond the ones listed in Design: Backlog. README content that has no
  page yet moves to `docs/contributing/` or stays reachable from GitHub.
- Generated CLI reference from `--help`: Backlog.

## Decisions

1. **Name and line:** "Agentic Warden: One policy for every coding agent" in the README H1, the docs and
   the site. The repo directory and the Go module path stay as they are (user, 2026-10-24).
2. **Primary reader:** platform or security lead evaluating a purchase (user, 2026-10-24).
3. **Docs source format** (one-way door, two shapes considered):
   - A. One TSX page per doc, content in `web/content/*.ts` like today's site pages.
   - B. Markdown files in `docs/site/*.md`, rendered by the Next site at build time with one small
     markdown dependency, also readable on GitHub.
   **Chosen: B.** One source for GitHub and the site, prose is editable without TSX, and it adds one
   dependency, not a framework. A follows the existing pattern but puts long prose in TS strings.
4. **Quickstart:** the CPO's rootless 7-step path (r2-cpo.md), from clone and build to the
   edit-and-reapply loop. There is no `make demo`: the reader runs each command, which is the proof.
   Step 3 applies `examples/quickstart.yaml` with two rules: deny `Read(./.env)` everywhere, and deny
   `Bash(curl *)` in `github.com/acme/payments*`. Two local repos (`payments-api`, `sandbox`) are
   created with one `git init` loop.
5. **GIFs:** one per quickstart step, as the founder asked. CLI steps are `vhs` tapes in
   `docs/tapes/`, rendered by a CI job, so a CLI change re-renders the GIF instead of leaving it stale.
   Agent steps (the refusal) are recorded from a real Claude session: T1 tries a scripted `vhs`
   recording with the local `claude`; if that fails, the user records by hand from the script in
   `docs/tapes/README.md` (user, 2026-10-24).
6. **Hero GIF:** `aw claude` in `payments-api` refusing `curl`, and the same request running in
   `sandbox`. Rootless and reproducible from the quickstart, captioned with the exact command.
   The plain-`claude` proof (needs root, on a test VM) has its own page and its own clip.
   (Approved 2026-10-24 with the spec; the CPO preferred plain `claude`.)
7. **Docs nav label:** "Security review" (the buyer's words, CMO) instead of "Evaluate" (CPO).
8. **The doctor "evaluation mode" line:** rootless `aw doctor` today prints 6 warnings on a healthy
   evaluation setup, so the reader's step 5 opens with red output. When the state dir is not the system
   default, `aw doctor` prints one line saying it is in evaluation mode and the warnings are expected.
   It still prints the warnings, and `--json` is unchanged.
9. **One call to action:** "Bring your rules" → `/demo`, using the site's existing words.
10. **Fact-check before print:** no claim ships without a source in code, tests or a run log. These
    are measured, not assumed: "about 10 minutes" (timed run, T1), the refusal wording (real capture,
    T1), and what removal leaves on a machine (T1).

## Design

### README (60 lines or fewer)
Start from the CMO r2 draft, fixed per the CTO critique (2026-10-24): centered header (logo from
`web/app/icon.svg`, H1, line, license and CI badges), hero GIF directly under it, policy YAML in plain
block style (no flow-style braces), a short "Try it" teaser whose commands are copied from the tested
quickstart and link to it, a tighter status-quo table, and a console screenshot if the self-hosted
console can be shown with demo data. Then: 3-sentence lede, status line, compact policy YAML,
the dated 3-row status-quo table (mechanisms only, no named vendors), 3 trust facts, a "Try it"
block with real commands, and links: Quickstart · Enforce on plain `claude` · Security model ·
Bring your rules · License. The current README sections move as follows:
- Why this exists: deleted (the gap essay); the table covers it.
- How policy reaches the agent, Bundle signing, KMS: `docs/site/how-it-works.md` and `docs/site/security-model.md`.
- Layout, Tests, Marketing site, Maintenance: `docs/contributing/README.md`.
- Try it: `docs/site/quickstart.md`.
- Security posture: `docs/site/security-model.md`.
- Admin console, Hosted cells: `docs/site/admin-console.md` and the existing `docs/hosting/`.

### `/docs` on the site
Rendered from `docs/site/*.md`. The nav order and frontmatter (`title`, `section`, `order`) live in
the markdown. Pages in this milestone:
- **Get started:** Overview (`index.md`, the CMO landing copy and 3 cards) · Quickstart ·
  Enforce on plain `claude` · How it works
- **Security review:** Security model and limits · Threat model · Trust statement (renders
  `docs/hosting/trust.md` or links to it) · Removing Agentic Warden
- **Guides:** Write your first policy · Admin console (self-hosted)
- **Reference:** Environment variables and files written per OS (moved from the README)

Each quickstart step has: the command with a copy button, the GIF, "You should see" output, and a
"why" link into Security review. Every page ends with the "Bring your rules" band.

### Site consistency fixes
- Console status: "available self-hosted; hosted console coming soon" (`web/content/*`), after
  checking the hosted-cells state in code.
- The signing claim gets "detects tampering, cannot stop a root user", linked to the security model.
- `site.docsUrl` points to `/docs`. The header gets a Docs link.
- The README stops promising a missing file: `docs/site/threat-model.md` exists.

### Product changes (Go, small)
- `examples/quickstart.yaml` (two rules, see Decision 4).
- The `aw doctor` evaluation-mode line (Decision 8).

### GIF pipeline
`docs/tapes/*.tape` (one per CLI step, from a clean temp `HOME`), plus a `docs` CI workflow that builds
the binaries, installs `vhs`, renders the tapes to `docs/assets/*.gif` and fails if the output differs
from the committed GIFs. Whether rendering is deterministic enough to diff is settled in T1; the
fallback is that CI re-renders and uploads an artifact without diffing.

## Acceptance criteria

1. README is 60 lines or fewer, its H1 is `# Agentic Warden`, and the line "One policy for every coding agent" appears:
   `test $(wc -l < README.md) -le 60 && head -3 README.md | grep -q 'One policy for every coding agent'`.
2. The string `agent-wrapper` does not appear in README.md or `docs/site/` except in the clone
   URL, directory or module path: `grep -n 'agent-wrapper' README.md docs/site -r` reviewed, 0 prose hits.
3. Every command in the quickstart runs as written, top to bottom, in a clean temp `HOME` without root, and
   each "You should see" line matches real output: `scripts/check-quickstart.sh` (extracts the fenced
   commands from `docs/site/quickstart.md` and runs them; the Claude steps are skipped and marked) exits 0.
4. On step 6, `aw claude` in `payments-api` refuses `curl` and in `sandbox` runs it. Evidence is a
   real recording or a capture log at `docs/tapes/capture-step6.log` with the exact refusal text,
   and that text is the text quoted on the page.
5. Every step that has a `.tape` has a committed GIF, and the CI workflow renders all tapes
   successfully: `ls docs/tapes/*.tape` and `ls docs/assets/*.gif` correspond 1:1 by name; the `docs`
   workflow passes on the PR.
6. `/docs` and every page listed in Design render, the nav shows the 4 sections in order, and no
   internal link is broken: `cd web && pnpm test:e2e -g docs` passes. It visits each page, checks the h1,
   and checks every internal href returns 200.
7. Web checks still pass: `cd web && pnpm lint && pnpm typecheck && pnpm test`.
8. Rootless `aw doctor` (non-default state dir, healthy setup) prints the evaluation-mode line and
   `--json` output is unchanged: `go test ./cmd/aw/... -run TestDoctorEvaluationMode -v`.
9. The quickstart claims a time only if a timed run supports it: the timing from T1's run log
   is ≤ the printed number of minutes.
10. No README promise points to a missing file: every relative link in README.md resolves
    (`scripts/check-links.sh README.md` exits 0).
11. The site no longer contradicts the code: the console wording and signing caveat are updated. The
    `panel-correctness` seat checks the claims on Security review pages against code and tests and
    reports 0 unsupported claims.
12. Go suite unchanged: `go test ./...` passes.
13. The README "Try it" commands also pass the quickstart check: `scripts/check-quickstart.sh README.md`
    exits 0 (same extractor, same rules as AC3).

## Security notes

There's no new trust boundary in code. The risk is security claims in the docs. The Security review pages
(security model, threat model, limits, removal) are checked claim by claim against code and tests in
the milestone panel (correctness seat), and the security seat reviews the threat model's content.
Recordings must not show tokens or a real account email. Use `you@acme.com` and the throwaway admin
token `try-me`.

## Status (converged 2026-10-25)

Checked on branch `docs-onboarding` at 1d5a5f7 against `main`. Commands were run on 2026-10-25.

| # | criterion (short) | verdict | evidence (command + key output line) |
|---|---|---|---|
| 1 | README 60 lines or fewer, H1, tagline | VERIFIED | `wc -l < README.md` = 59; `head -3` shows `# Agentic Warden` and `**One policy for every coding agent.**` |
| 2 | no `agent-wrapper` prose in README or docs/site | VERIFIED | `grep -rn 'agent-wrapper' README.md docs/site` gives 0 prose hits; all hits are filesystem paths (`~/.cache/agent-wrapper`, `/var/lib/agent-wrapper`, `<user-cache>/agent-wrapper/...`) |
| 3 | quickstart commands run; checker exits 0 | VERIFIED (pre-push) | `GIT_CONFIG_COUNT=1 ... insteadOf ... scripts/check-quickstart.sh docs/site/quickstart.md` → `ok (10 run, 2 skipped, 24 expected lines found)`, exit 0 |
| 4 | step 6 refusal in payments-api, run in sandbox, log + quoted text | VERIFIED | `docs/tapes/capture-step6.log` step 1 and step 6 (payments-api) "Command didn't run. Permission to run `curl -sI https://example.com` was denied"; step 5 (sandbox, `--allowedTools "Bash(curl *)"`) prints `HTTP/2 200`; the quickstart page quotes the same sentence verbatim |
| 5 | each `.tape` has a GIF 1:1; CI renders all tapes | INCONCLUSIVE | 1:1 part VERIFIED: `ls docs/tapes/*.tape docs/tapes/manual/*.tape` = 01,02,03,04,05,07 + manual/06-claude, manual/hero; `ls docs/assets/*.gif` = the same 8 names. CI pass not run: needs the PR pushed (no `docs` workflow run exists) |
| 6 | `/docs` pages render, 4 sections in order, no broken link | VERIFIED | `cd web && pnpm test:e2e -g docs` → `9 passed`, incl. "nav shows the sections that have pages in the fixed order" and "every internal /docs link returns 200" |
| 7 | web lint, typecheck, unit tests pass | VERIFIED | `pnpm lint` exit 0; `pnpm typecheck` exit 0; `pnpm test` → `Tests 81 passed (81)` |
| 8 | rootless `aw doctor` evaluation line; `--json` unchanged | VERIFIED | `go test ./cmd/aw/... -run TestDoctorEvaluationMode -v` → `--- PASS`; test asserts the line precedes the warnings, is absent from `--json` and absent for the default state dir |
| 9 | any printed time claim backed by a timed run | VERIFIED (no claim) | `grep -rn -i '10 min\|ten minutes\|about 10' docs/site README.md web/content web/app` → no hits; only "five minutes" for the timer interval in `enforce-plain-claude.md`, not a quickstart time claim |
| 10 | every README relative link resolves | VERIFIED | `scripts/check-links.sh README.md` exit 0 |
| 11 | panel-correctness: 0 unsupported claims on Security review pages | INCONCLUSIVE | Ledger shows 14 unsupported claims found, fixed in W1 and re-reviewed (`docs/superpowers/plans/2026-10-24-docs-onboarding.md`, "Milestone panel triage"). Spot-check of `security-model.md`: cited tests exist (`TestFetchRejectsATamperedBody` in `internal/sync/client_test.go`, `TestEveryRunIsAudited` in `internal/policyhelper/helper_test.go`, `TestRunRefusesATamperedBundle`). No final 0-findings seat report is in the ledger. Missing run: a final panel-correctness pass on this diff |
| 12 | `go test ./...` passes | VERIFIED | `go test ./...` exit 0; 0 FAIL lines |
| 13 | README "Try it" passes the quickstart checker | VERIFIED (pre-push) | `GIT_CONFIG_COUNT=1 ... scripts/check-quickstart.sh README.md` → `README.md ok (2 run, 0 skipped, 1 expected lines found)`, exit 0 |

### Deviations and notes

- AC3 and AC13 ran with `GIT_CONFIG_*` `insteadOf` pointing the clone at the local checkout, because GitHub `main` lacks `examples/quickstart.yaml` until this branch merges. No global git config was written. An unredirected re-run is due after the push.
- AC5: the `docs` workflow has not run on a PR. The GIFs for `06-claude` and `hero` were rendered by hand with a real login (commit 8b67b5e); this convergence did not re-render them.
- AC4: the capture log is dated 2026-10-24 and was appended in fix rounds. The quoted refusal sentence matches the log exactly.
- AC2: the `agent-wrapper` hits are state-directory paths the product writes, not product-name prose. Read as the spec's "directory or module path" exception.
- Decision 10 asked for a timed run before any time claim. Since no page prints a time, no timed run was needed for AC9.
