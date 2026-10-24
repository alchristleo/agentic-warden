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

1. Steps 1-5 and 7 run rootless in a temp HOME exactly as written (capture.log). Exact outputs: `applied revision quickstart.1 (2 rules)`, `enrolled machine <id> for you@acme.com`, `synced bundle quickstart.1: wrote 3 files`, `note: policy compiled for github.com/acme/payments-api`. `examples/quickstart.yaml` did not exist (T2), so it was written to the plan's T2 spec.
2. Time: steps 1-5 take 6.8 s of machine time (cold clone+build 5.6 s with a warm module cache, 0.7 s warm; awd start 1 s; steps 3-5 under 0.2 s each). A reader's time is typing plus a first-time `go build` with a cold module cache (not measured). "About N minutes": print only a reader estimate, not these seconds. Step 6 unmeasured.
3. Step 6 NOT run live: `claude -p` in a temp HOME answers "Not logged in", so it needs the real ~/.claude login. Hand-recording script written (README.md). `aw` passes args through, so `aw claude -p "..."` works for that run. Refusal wording is unmeasured. The compiled settings prove the split: payments-api denies `Read(./.env)` + `Bash(curl *)`, sandbox only `Read(./.env)`.
4. Step 7 verified without Claude: edit + `awd apply` + `aw-sync once` -> payments-api gets `Bash(wget *)` too. After `awd` is stopped, `aw-sync once` exits 1 ("connection refused"), the files are unchanged and `aw doctor` still compiles the same deny (fails safe). Doctor shows the sync error line.
5. vhs: `go install github.com/charmbracelet/vhs@latest` works without sudo (v0.12.1). `ffmpeg` and `ttyd` are absent here and are not in Go; only a downloaded static binary would supply them, and that download was blocked by this session's permission policy. vhs refuses to run ("ffmpeg is not installed"). Determinism NOT measured: the CI diff vs artifact decision stays open; the safe choice is artifact (do not diff GIF bytes in CI).
6. Step 5 is NOT quiet today: `aw doctor` also prints 6 claude warnings and `error:` lines for codex and gemini not on PATH (a trial machine usually lacks them). T2's evaluation line alone will not remove the errors; the quickstart page should say to expect them or T2 should scope doctor to `--agents claude`.
7. Removal (removal-notes.md): rootless trial = `rm -rf ~/.aw-try ~/.cache/agent-wrapper` plus the clone. Root setup writes /etc/claude-code (bundle + 50-agent-wrapper.json drop-in), /etc/codex, /etc/gemini-cli, /var/lib/agent-wrapper, systemd units, /usr/local/bin. There is no uninstall command; `uninstall-timer` leaves files and enrollment.
8. Hosted console: not reachable by users. Site says "Coming soon" (product.ts, pricing.ts "Not yet"); docs/hosting/runbook.md is an operator runbook for design-partner cells with an optional console. Verdict: write "Hosted console: coming soon; self-host `awd` today, `/console/` is opt-in via AWD_CONSOLE_*".

Coordinator follow-ups: (6) fixed in T2 b03c150 (doctor checks enrolled agents only). (3) user approved 2026-10-24: capture step 6 with the real login. (5) user will install ffmpeg+ttyd; CI uses artifact, no byte-diff. (2) user: no minutes claim; say "seven commands, no root".

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
T4 tiny fix 4148669 (13/13). Group A merged; go test ok, web 79/79, lint+typecheck ok
T1b dispatched (step6 capture, real login, base 16454f0); T5b dispatched (security pages)
T1b done cc3813a: .env denied both; curl denied in payments even with --allowedTools; sandbox needs --allowedTools under -p. Step 6 = same cmd w/ --allowedTools in both repos
T5b done 899a067 in review; discloses 6 product security gaps (unauth GET /v1/policy, aw-policy fail-open default, no bundle revision/expiry -> rollback, X-Applied-By actor, no rate limit, plain HTTP)
T5b review=fix-needed (2 must overclaims: signing vs stale/copied bundle, sync all-or-nothing); fix r1 sent. T6 note: README L146-147 + site signing claim share overclaim
T5b fix r1 5bdf873 re-review=fix-needed (coordinator brief error: Version field exists, unchecked); fix r2 sent
T5b done c8e2190 fix r2 verified by coordinator grep vs bundle.go; merged
T5a done e467bd7 in review (step6 prompt-first arg order; aw-sync status needs --state-dir; CI needs stub claude)
T5a review=fix-needed; dismissed: main lacks quickstart.yaml (pre-merge only; recheck AC3 without insteadOf post-merge); fix r1 sent
T5a done 7e2a7ec merged; link musts dismissed (T5b merged, GIFs = T7); machine id redacted by coordinator
T6, T7 dispatched base c81e096 (T7 stops before render: ffmpeg/ttyd pending user)
T7 done e63880b (tapes valid 8/8, not rendered) in review
T6 done 872749e in review (README 58 lines; no Bring-your-rules link: site URL unknown -> ask user)
T7 review=fix-needed (must: manual prep writes real ~/.gitconfig); fix r1 sent
T6 review=fix-needed (signing conditional, fail-open honesty, product.ts "Fails safe" title -> "Keeps working when the server is down" [coordinator wording, surface at merge gate]); fix r1 sent
T7 done f2caea6 fix verified by coordinator (no global git config; kills; triggers)
T6 done 5c6d292 (blank-line nit skipped: breaks AC1)
All T merged aa4c25e; suite green. Step 7 panel dispatched: correctness, design, security, browser QA

## Milestone panel triage (2026-10-24)

Seats: correctness (AC11: 14 unsupported claims), design, security, browser QA. One fix wave, 3 parallel agents (disjoint files), then one sdd-reviewer-lite re-review.

**W1 docs content** (docs/site/*.md, README.md, web/content/*.ts) · sdd-implementer
- [user 2026-10-24] threat-model.md + security-model.md: split disclosed gaps into "Design limits" (fail-open default, plain HTTP, TOFU key pin, policy write = fleet code exec, client-side not a boundary), each with the operator mitigation, and "Known issues, fix scheduled" (unauth GET /v1/policy, X-Applied-By audit actor, no rate limit, unchecked Version rollback): impact + workaround, no exploit steps.
- [user 2026-10-24] web/content/product.ts title -> "Fails open by default, closed when you choose" (replaces T6 retitle); update the e2e title assertion.
- [must, corr] security-model.md:36-38 "does prevent" list false: aw-trust.pub lives in the state dir, so a state-dir writer swaps key+bundle+sig or deletes the key (inspect.go:261-265). Keep only "impersonated control plane on the fetch path".
- [must, sec] Key pinned trust-on-first-use at enrollment (aw-sync main.go:234, client.go:89-127): over plain HTTP an in-path attacker can swap/strip it. Say enrollment+sync need TLS or an out-of-band key-id check; threat-model rows :28 :34 :45, security-model :20. No exploit steps.
- [must, design+corr] Stale README pointers: security-model.md:42,57; threat-model.md:68-70 -> how-it-works/write-first-policy/admin-console; the "no test" rows have tests: TestBundleGroupsIncludeActiveSCIMGroups, TestResolveFlagsACaseMismatch, TestAuthoredGroupsNeverCount.
- [should, sec] New E row: policy write = code execution on every enrolled machine (managed accepts hooks, env, apiKeyHelper, mcpServers) -> admin token, console admin, signing key are fleet-root.
- [should, corr] security-model:22 "re-checks every launch" only on a signed deployment; :14 precedence claim needs a source; threat-model:46 "all-or-nothing" -> "nothing written unless every renderer and validation pass".
- [should, corr] Signing applies only to machines enrolled after the key is set (client.go:199, inspect.go:335): README.md:31, web/content/home.ts:61. README.md:11 hero alt text says .env but hero.tape records curl: fix the alt.
- [should, corr] removing.md:14,18,27,49: add ../payments-api ../sandbox, per-developer ~/.cache/agent-wrapper/aw-policy/aw-policy.log, /etc/claude-code/aw-policy.json; .aw-revision only beside .json files. reference.md:55-56: .sig and aw-trust.pub are in the state dir.
- [should, sec] security-model.md:16 AW_SYNC_STATE_DIR "awtest only" applies to aw-policy; aw honors it by design (cmd/aw/main.go:395).
- [should, sec -> elevated must by coordinator: cheap, high impact] Quickstart: `export AWD_ADDR=127.0.0.1:8080` and `export AWD_ADMIN_TOKEN=$(openssl rand -hex 16)` (export form is allowed by the checker) replacing `try-me`; same in README try-it and enforce-plain-claude.md:30. W2 mirrors it in prep.sh and tapes.
- [should, design] index.md: drop the clone block and "All pages"; keep pitch, hero, prerequisites, Start, three cards. Drop the "Bring your rules" footers from markdown (W3 renders it once in the docs layout).
- [should, design] Remove reader-facing links to docs/tapes logs (quickstart.md:160, :293); merge the three step 6 prompts into one block and one sample output; drop "Why" lines.
- [nit] wording drift: enforce-plain-claude.md:38 doctor still warns "enrolled before bundle signing" when unsigned; quickstart.md:293 "fails safe" -> "keeps working"; how-it-works.md:66 audit is best effort; console needs AWD_CONSOLE_* plus AWD_PUBLIC_URL (product.ts, pricing.ts); index/quickstart duplicate subtitle sentence; quickstart step 2 expected block ends "listening on" (complete or trim).

**W2 scripts, CI, tapes** (scripts/, .github/workflows/, docs/tapes/) · sdd-implementer
- [must, design+corr] Run scripts/check-links.sh in CI on README.md and docs/site/*.md (needs the GIFs committed before merge; AC5).
- [should, corr] check-quickstart: fail on a text block that is not directly after a checked sh block or a skip (quickstart.md:112 step 4 was never checked); trap INT TERM EXIT to kill the whole process group.
- [should, corr] docs.yml paths add README.md; web.yml paths add docs/** (AC6 e2e).
- [should, design] Split tape rendering into its own job/workflow on workflow_dispatch + docs/tapes/**; keep the quickstart checks on the wide filter. Delete the dead "Build the binaries" step.
- [should, design] Drop AW_STUB_BIN: prep.sh writes the stub claude itself unless AW_MANUAL=1; remove the workflow stub step and the README recipe.
- Mirror W1's AWD_ADDR + random token in prep.sh and the tapes. Delete docs/tapes/removal-notes.md and t1-findings.md (duplicates of removing.md and the plan). Keep capture*.log (AC4 evidence). Inline the contract in the check-quickstart.sh header instead of citing the plan.

**W3 web + Go** (web/app, web/lib, web/components, cmd/aw) · sdd-implementer
- [must, QA] Mobile 390px: wrap docs tables in overflow-x:auto; overflow-wrap:anywhere on inline code.
- [should, corr] /docs-assets route reads ../docs/assets at request time and is not traced on serverless deploys: make it static (generateStaticParams + force-static) or copy assets into public/ at build; add an e2e that fetches a real asset (logo.svg).
- [should, QA] copy button overlaps the first line on mobile; mobile header has no Docs link; docs nav collapsible on mobile (details/summary) and aria-current + active style on the current page; render the "Bring your rules" band once in the docs page layout.
- [nit, sec] escapeHtml escapes `"`; [nit, design] ASSET_TYPES = gif, svg; spacing `|| !assetType`.
- [nit, corr] cmd/aw/main.go:333 compare filepath.Clean of both paths, with a test.

**Dismissed:** delete exec-room papers and .agents context (user chose to commit both, 2026-10-24); delete capture-step6.log (AC4 evidence); actions pinned by tag not SHA (matches the repo's other workflows; no secrets, contents: read).
**Backlog (design shoulds, larger):** dedupe security-model vs threat-model (one home per claim); make docs/hosting/trust.md the docs page itself; move how-it-works per-agent detail to Reference.
**Product security gaps for the user** (out of scope here): unauth GET /v1/policy; aw-policy fails open by default; Version unchecked (rollback); X-Applied-By audit actor; no rate limit; plain HTTP; TOFU key pin; policy write = fleet code execution.

PAUSED at step 7 (panel triaged, fix wave not started), next: dispatch W1, W2, W3 in parallel (worktrees, base = HEAD of docs-onboarding), then sdd-reviewer-lite re-review, render GIFs (user installs ffmpeg ttyd; manual tapes with real login), step 8 spec-converge, merge gate.
RESUMED 2026-10-25: W1, W2, W3 dispatched base 81dc385
W2 done 05f99a0 test-checks 14/14; kept CI stub claude (step 5 doctor needs claude on PATH; accepted); tapes not rendered
W3 done b488f50 web 81 + e2e docs 9 + go ok; 390px not eyeballed (e2e overflow check); one-band assert after W1 merge
W1 done a922ec8 quickstart+README checks ok, web 79; precedence claim reworded (no source); mcpServers not claimed; follow-ups: og image title, 'top of settings stack' in enforce-plain-claude+quickstart
coord fix a20422c: W1 put user title on page H1, moved to fail-safe card (H1 restored); precedence wording in enforce-plain-claude+how-it-works; copy e2e -> /docs/quickstart; one-band assert. e2e 47/47. 390px eyeballed ok; backlog: table env names break mid-word
rereview fix-needed: 2 quickstart text-block parse (fixed coord), README apply (dismissed: GitHub main lacks examples/ pre-merge, passes with insteadOf; recheck post-push), capture.log note, TLS source. check-quickstart both ok
GIFs rendered 8b67b5e: 6 CI + 06-claude + hero (real login, ~/.aw-try removed, new cache files removed); vhs needs ttyd>=1.7.2 (apt has 1.6.3; static 1.7.7 used); prompt shows cwd; check-links ok
AC11 recheck: ~230 claims, 9 unsupported (1 must email_verified missing-claim accepted; client-swap no admin needed; enroll --force moves pin; step6 evidence is aw claude not helper; doctor dir check; group case; macOS cache path; README fail-open scope; removal step). Fix dispatched sdd-implementer-lite
AC11 fixes 99dc354 (sdd-implementer-lite); AC11 -> VERIFIED. Product gaps now 9 (+ email_verified missing claim accepted); design limit: own Claude Code client. Next: merge gate
PR #3 opened 2d59186; waiting CI (AC5)
PR #3 CI all green (docs x2, tapes, test x2); AC5 VERIFIED. Next: user merges PR, then AC3/AC13 unredirected recheck, retro
MERGED 2026-10-25 PR #3 (4072065). AC3/AC13 re-run against GitHub main, no redirect: ok. All 13 ACs VERIFIED.

## Retro (2026-10-25)

- **Numbers:** 9 task dispatches (T1-T7, T1b, T5b), 8 fix rounds, 0 blocked; panel 6 must-fix + 1 user-elevated, fix wave 3 agents + 1 claims re-check (9 more unsupported claims); 5h usage 97% at pause, 36% at end (after reset).
- **Wasted:** a fix agent applied the user's title to the page H1 instead of the feature card (brief said "product.ts title"). Structure: briefs quote a user decision with its exact file:line or field.
- **Wasted:** W2 tightened check-quickstart while W1 edited quickstart.md in parallel; the break showed only in re-review. Structure: after merging a parallel group, run every CI job command (docs.yml checks), not just test suites.
- **Wasted:** AC11 stayed INCONCLUSIVE until a scoped seat re-run, which found 9 more claims. Structure: an AC that rests on a panel verdict gets a scoped re-run of that seat after the fix wave, before spec-converge.
- **Kept:** vhs needs ttyd >= 1.7.2 (apt has 1.6.3): pinned in tapes.yml and noted in docs/tapes/README.md.
