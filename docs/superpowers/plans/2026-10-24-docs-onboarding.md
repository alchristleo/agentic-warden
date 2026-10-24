# Plan: docs and onboarding

Spec: `docs/superpowers/specs/2026-10-24-docs-onboarding-design.md` (approved 2026-10-24).
Branch: `docs-onboarding`. Base: `27fbfb7` + spec fold-in commit.
Exec-room inputs: `docs/superpowers/exec-room/2026-10-24/r2-*.md` (quickstart table in r2-cpo.md,
README and landing copy in r2-cmo.md). Product context: `.agents/product-marketing.md`.

## Shared contracts

**Quickstart markdown contract** (producer: T5a writes `docs/site/quickstart.md` and T6 the README;
consumer: T4 `scripts/check-quickstart.sh`):
- A fenced block tagged ```` ```sh ```` is run, in order, in one bash session (`set -euo pipefail`),
  in a temp `HOME` and working dir.
- A ```` ```text ```` block directly after a `sh` block lists the expected output. Every non-empty
  line in it must appear as a substring of that block's combined stdout+stderr.
- An HTML comment `<!-- check: skip (reason) -->` on the line before a `sh` block skips it. Skipped
  blocks are printed with their reason, never silently dropped. Use this only for steps that need a
  logged-in Claude or root.
- Background processes started in a block (`awd serve &`) are killed when the script exits.

**Docs page frontmatter** (producer: T3 renderer; consumers: T5a, T5b, T6):
```yaml
---
title: Quickstart
section: get-started
order: 2
description: One line, used for meta and cards.
---
```
`section` is one of `get-started`, `security-review`, `guides`, `reference`. Values are plain `key: value` lines (no inline comments; one pair of surrounding quotes is stripped).
The slug is the filename without `.md` (`docs/site/quickstart.md` → `/docs/quickstart`), and
`index.md` maps to `/docs`. Pages are written to read well on GitHub: link pages as `./<slug>.md`
(optionally `#anchor`) and images as `../assets/<file>`. The renderer rewrites those to `/docs/<slug>`
and `/docs-assets/<file>` (served from `docs/assets/`); absolute and external links are left alone.
A leading `# Title` is dropped (the page renders the title as h1); any other `#` heading renders as h2.

**Tapes** (producer: T7): `docs/tapes/<NN>-<name>.tape` → `docs/assets/<NN>-<name>.gif`. The hero GIF
is `docs/assets/hero.gif`.

## Tasks

### T1: Capture and measure (settles pending facts) · agent: sdd-implementer · depends-on: none
Run the r2-cpo.md quickstart from a fresh clone build in a temp `HOME`, without root, and record:
- the exact output of every step, saved as `docs/tapes/capture.log`;
- the wall-clock time from the first command to step 6, done as a reader would. Typing is not
  simulated, but note how long each step took;
- step 6: run `aw claude` in `payments-api` and `sandbox` non-interactively if `aw` passes args through
  (`-p "run curl -sI https://example.com"`), else interactively under `vhs`. Save the refusal text
  and the allowed run to `docs/tapes/capture-step6.log`. If Claude auth or interactivity blocks this,
  stop trying after 3 attempts and write the hand-recording script for the user instead
  (`docs/tapes/README.md`, a section titled "Recording the Claude clips");
- `vhs`: can it be installed without sudo (`go install` plus `ttyd`/`ffmpeg` availability)? Render
  one trivial tape twice and report whether the GIF bytes are identical (this settles the CI diff
  vs artifact decision);
- removal: what files `aw-sync` writes for a rootless setup and for a root setup (read the code), and
  what removing Agentic Warden means. Save to `docs/tapes/removal-notes.md`;
- hosted console state: is a hosted console reachable for users today (read code and
  `docs/hosting/`)? Give a one-line verdict for the site wording fix.
Outputs: `docs/tapes/capture.log`, `capture-step6.log`, `removal-notes.md`, and
`docs/tapes/t1-findings.md` (15 lines or fewer; the coordinator copies it into this plan).
Constraints: no sudo, no writes under `/etc` or the real `$HOME`, the admin token is `try-me`, the
email is `you@acme.com`, and no real account details appear in any log.
Acceptance: the logs exist; the T1 findings answer all six questions with evidence.

### T2: Quickstart policy and doctor evaluation line · agent: sdd-implementer · depends-on: none · parallel group A
Files: `examples/quickstart.yaml`, `cmd/aw/` (the doctor code), and its tests.
- `examples/quickstart.yaml`: two rules, `baseline` (claude deny `Read(./.env)`) and
  `payments-repos` (match `repos: ["github.com/acme/payments*"]`, claude deny `Bash(curl *)`). It is
  plain block-style YAML and must be valid against the existing policy compiler.
- `aw doctor`: when the resolved state dir is not the system default, print the line
  `evaluation mode: state dir <path> is not the system default; the warnings below are expected for a rootless trial`
  before the warnings. The warnings are unchanged and `--json` output is byte-identical.
Tests: `TestDoctorEvaluationMode` (the line is present for a non-default state dir and absent for the
default; JSON is unchanged) and `TestQuickstartPolicyCompiles` (the example compiles; `payments-api`
gets both denies and `sandbox` gets only the `.env` deny).
Acceptance: `go test ./...` passes.

### T3: `/docs` renderer on the site · agent: sdd-implementer · depends-on: none · parallel group A
Files: `web/app/docs/` (layout with section nav, `[[...slug]]/page.tsx`), `web/lib/docs.ts` (loader:
reads `../docs/site/*.md` at build time, parses frontmatter, groups and orders the nav), a route
serving `docs/assets` at `/docs-assets/*`, a copy button on code blocks, the header Docs link,
`site.docsUrl` → `/docs`, and `web/tests/e2e/docs.spec.ts`.
- Add one small markdown dependency (e.g. `marked` or `remark` + `remark-html`) and a frontmatter
  parser; no MDX or docs framework. Pages are statically generated.
- Seed `docs/site/index.md` with a placeholder page so the route builds; T5a replaces it.
- Style it with the existing site components and the theme, matching the product page typography.
Tests: `docs.spec.ts` visits every page returned by the loader, checks the h1 equals the frontmatter
title, checks the nav shows sections in the order Get started, Security review, Guides, Reference
(only those that have pages), and checks every internal `/docs` href returns 200. A unit test for the
loader covers frontmatter parsing, ordering, and rejecting an unknown section.
Acceptance: `cd web && pnpm lint && pnpm typecheck && pnpm test && pnpm test:e2e -g docs`.

### T4: Check scripts · agent: sdd-implementer-lite · depends-on: none · parallel group A
Files: `scripts/check-quickstart.sh`, `scripts/check-links.sh`, and `scripts/testdata/` fixtures.
- `check-quickstart.sh <file.md>` implements the quickstart markdown contract above. It assumes the
  binaries are on `PATH` (the first `sh` block in the real quickstart builds them).
- `check-links.sh <file.md>` checks that every relative link and image path resolves to a file in the
  repo (absolute `/docs/...` site links map to `docs/site/<slug>.md`). External URLs are skipped.
Tests: `scripts/testdata/` with a passing md, an md whose expected output is missing (exit ≠ 0, and
the message names the block), an md with a skip comment (exit 0, the skip is printed) and an md with a
broken link. Add `scripts/test-checks.sh`, which runs all four and asserts the exit codes.
Acceptance: `scripts/test-checks.sh` exits 0.

### T5a: Pages for Get started, Guides and Reference · agent: sdd-implementer · depends-on: T1, T2, T3, T4
Files: `docs/site/index.md` (CMO landing copy: lede, 3 cards, CTA), `quickstart.md` (7 steps per
r2-cpo.md, using the real outputs from `capture.log`, with GIF slots `/docs-assets/NN-name.gif` and
"why" links into Security review), `enforce-plain-claude.md` (test VM, sudo, stop `awd`, undo),
`how-it-works.md` (from README "How policy reaches the agent" plus the status-quo table),
`write-first-policy.md`, `admin-console.md` (from README), `reference.md` (environment variables
and files written per OS, from README and code), and `docs/contributing/README.md` (README Layout,
Tests, Marketing site, Maintenance, moved verbatim and then fixed).
Constraints: every claim has a source in code, tests or `capture.log`; prefer moving README text
over rewriting it; use the copywriting skill rules (no AI tells); every page ends with the
"Bring your rules" band (a markdown line linking `/demo`); print "about N minutes" only from T1's
timing.
Acceptance: `scripts/check-quickstart.sh docs/site/quickstart.md` exits 0;
`scripts/check-links.sh` exits 0 on every new file; `pnpm test:e2e -g docs` passes.

### T5b: Security review pages · agent: sdd-implementer · depends-on: T1, T3, T4 · parallel with T5a
Files: `docs/site/security-model.md` (from README Security posture, Bundle signing and KMS, with
limits stated plainly, including "detects tampering, cannot stop a root user"), `threat-model.md`
(STRIDE over the components: awd, the bundle, aw-sync, aw/policyHelper, the IdP sync and hosted
cells; for each, the threat, the mitigation in code and the residual risk), `trust.md` (render or
link `docs/hosting/trust.md`; don't fork its content), and `removing.md` (from
`removal-notes.md`).
Constraints: every mitigation names the file or test that enforces it; mark residual risks honestly.
Acceptance: `scripts/check-links.sh` exits 0 on each; `pnpm test:e2e -g docs` passes.

### T6: README rewrite and site consistency · agent: sdd-implementer · depends-on: T5a, T5b
Files: `README.md`, `web/content/*.ts`, and anything else that links to the README sections that moved.
- README: 60 lines or fewer, per the spec's README design (the CMO r2 draft plus the CTO fixes):
  centered header with logo and badges, hero GIF, block YAML from `examples/quickstart.yaml`, a
  tighter status-quo table, 3 trust facts, a "Try it" teaser copied from the quickstart, and links. Use
  the copywriting skill.
- Site: console wording per the T1 verdict; the signing caveat linked to `/docs/security-model`.
Acceptance: spec AC1, AC2, AC10 and AC13 commands pass; web checks pass.

### T7: GIF pipeline and recordings · agent: sdd-implementer · depends-on: T1, T5a
Files: `docs/tapes/*.tape` (one per CLI quickstart step, clean temp `HOME`), `docs/assets/*.gif`,
`.github/workflows/docs.yml` (build the binaries, install `vhs`, render, then diff or upload the artifact
per the T1 determinism finding; also run `scripts/test-checks.sh` and `check-quickstart.sh` on
`quickstart.md` and the README), and `docs/assets/hero.gif` plus the step 6 GIF from the T1 recording
when it succeeded. Otherwise use placeholder images with a visible "recording pending" label, and
the plan Backlog gets "user records Claude clips per docs/tapes/README.md".
Acceptance: spec AC5 (tapes ↔ GIFs 1:1); the workflow YAML passes `actionlint` if available.

## Parallel groups

- Group A: T1, T2, T3, T4 (no shared files). Dispatch in one message with worktrees.
- Group B: T5a and T5b (separate files under `docs/site/`).
- Then T6 and T7 (T7 needs only T1 and T5a, so it can start alongside T6).

## Backlog

- Release binaries, `install.sh`, module path rename (next milestone; swap quickstart step 1).
- Pricing to two paths (self-host, hosted pilot): founder call.
- Named-vendor comparison page: needs a founder-confirmed competitor scan.
- Search-entry pages ("per-repository rules for Claude Code", …).
- Quickstart completion signal (opt-in).
- Generated CLI reference from `--help`.
- Remaining guides: per-repo rules, IdP groups, Codex and Gemini, fleet rollout, signing and key rotation,
  self-host for production.
- Timed run by someone new before the quickstart claims a time again.

## T1 findings

_(copied from docs/tapes/t1-findings.md after T1)_

## Ledger
T1-T4 dispatched base=553e4d8 (group A, worktrees)
T2 done 82fee3b sdd-implementer review=approve (2 nits, kept)
T3 done 399cbd4 sdd-implementer in review (marked dep; sitemap omitted)
T4 done 43210d8 sdd-implementer-lite in review
T1 done 5751f68 sdd-implementer step6 not captured (no login in temp HOME); vhs needs ffmpeg+ttyd; doctor errors codex/gemini in step 5
T4 fix r1 sent: && failure silent (must), process-group kill, prefix-assignment $(...) (should)
T2 fix r1 sent: doctor checks enrolled agents only (T1 finding 5)
PENDING USER: run `! sudo apt-get install -y ffmpeg ttyd` (user outside, remind on return); T7 GIF render blocked on it
T2 fix b03c150 re-review=approve (3 nits)
T3 review=fix-needed (sitemap, frontmatter quotes/#, extra h1, link rewrite contract change: ./slug.md -> /docs/slug); fix r1 sent
T3 fix ca289c9 re-review=approve; rule for T5: links outside docs/site must be absolute GitHub URLs
T4 fix a5d4dbc re-review=approve; tiny follow-up (; false positive) sent
