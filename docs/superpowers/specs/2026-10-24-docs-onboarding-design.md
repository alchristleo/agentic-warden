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
