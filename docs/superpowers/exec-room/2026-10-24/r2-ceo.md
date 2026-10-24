# Exec room r2 — CEO: revised position

Date: 2026-10-24. Inputs: r1-ceo, r1-cpo, r1-cmo, plus the CPO's verified facts. The rootless chain
works today (`awd serve` → `apply` → `enroll-token` → `aw-sync enroll` → `aw-sync once` → `aw doctor`).
There are no release binaries. The module path `github.com/acme/agent-wrapper` breaks `go install`.
Agent GIFs need a logged-in Claude.

## What I changed and why

1. **Dropped my one-command seeded demo.** I adopt the CPO's 7-step rootless path instead. The CPO is
   right that a demo wrapper hides the steps an evaluator repeats in a real rollout. Those steps
   now work, so the wrapper is not needed. This is also simpler: nothing new to build or maintain.
2. **Kept my per-repo aha and moved it into the CPO's step 7.** A `.env` deny alone (CPO step 6) is
   something vendor managed settings already do. It proves the product works, but it does not prove
   why anyone would buy it. The edit loop in step 7 should add a **repo-scoped** rule (deny
   `Bash(curl *)` in `demo-repo` only), so the evaluator sees the same agent get two outcomes in two
   directories. That is our differentiator, shown in their own terminal. No extra step.
3. **Release binaries are now proposal #1.** I had assumed the quickstart was mostly content.
   It is not: with no binaries, every path starts with a clone and a Go build. That blocker comes
   before any copy work.
4. **The threat model becomes the "Security model and limits" page.** The README promises
   `docs/threat-model.md`, but nothing exists. The smallest honest fix is one page that covers both:
   client-side control, detection rather than prevention against root, Codex/Gemini repo rules
   advisory, the editable `.git/config` origin, claude.ai server settings shadowing the helper.
   The README then links that page. A formal STRIDE document comes later, if a buyer asks.
5. **Pull-the-plug proof moves to the "Enforce on bare claude" page** (test VM, sudo). That page is
   aha+, the purchase trigger for a security lead. A rootless laptop run cannot show it honestly.
6. **Conversion action unchanged; nothing to build.** The end of the quickstart and of every Evaluate
   page points to the existing /demo: "Bring the rules you enforce today", or "Request a sandbox
   cell" for a hosted pilot. We never say "Sign up", because the hosted console is not live.

## Resolved conflicts

### (a) GIFs: one per step, scripted. I withdraw "one recording only".
The founder asked for a short GIF per step. My objection was that GIFs rot. Scripted vhs tapes in
CI answer that: they regenerate on release, so they do not rot. That leaves no reason to overrule
the founder. Decision:
- **CLI steps (1–5, the apply/sync half of 7): vhs tapes in CI.** Deterministic, 3–6 s, regenerated per release.
- **Agent steps (6, the refusal in 7, bare `claude` on the enforce page): recorded by hand.** Three
  short clips, re-recorded only when the denial output changes. The denial text must be captured
  from a real run, never mocked. [NEED: CTO, confirm a recording shows no account, org or token details]
- **Hero = the step 7 per-repo clip** (two directories, two outcomes), reused on the README,
  /docs and the home page. That is the CMO's storyboard on the CPO's rootless path.
- Rejected: the CMO's Docker-based GIF 1 (Docker is no longer the evaluator path).

### (b) Founder-input items: both move out of this milestone
- **Pricing cut to two options: later.** It is not on the docs path, and it needs a founder decision
  plus pilot terms [NEED: pricing metric, pilot terms]. This milestone links /demo as it is.
- **Comparison page against vendor managed settings: later.** It needs a dated, founder-confirmed
  competitor scan [NEED]. This milestone needs only one sentence on the /docs overview naming the
  status quo ("today you'd push each vendor's managed settings by MDM, machine-wide, one agent at a
  time"). It names no rival and has nothing that goes stale. The README gap essay is deleted, not moved.
- **One name and one line ("Agentic Warden: One policy for every coding agent") stays in.** It is
  a doc edit and the site already uses it, so no new founder decision. The filename
  `50-agent-wrapper.toml` stays as it is (product code, not docs).

### (c) CPO's 7-step path and CMO's "Evaluate track first"
- **CPO 7-step path: agree**, with the one change above (step 7 adds a repo-scoped rule). Also agree
  on the CPO's two small CLI fixes: token from stdin, and the "evaluation mode" line in doctor in
  place of 6 red warnings. A security reader's first screen must not be red. The optional
  `aw-sync uninstall` cleanup goes on the enforce page only, if the CTO confirms it is small.
- **CMO "Evaluate track first": agree for the nav, disagree for build order.** The security lead
  should see "How it works" and "Security model and limits" near the top of /docs. But the
  quickstart is the critical path: without binaries and a working run, Evaluate pages describe a
  product nobody can try. Build order: binaries → quickstart → limits/enforce pages → README.
  Nav order: Overview, Quickstart, Evaluate (How it works, Security model and limits, Enforce on
  bare claude, trust.md), Guides, Reference, Contributing.

## Cuts, reaffirmed
README gap essay (deleted); contributor internals → `docs/contributing/`; curl + JSON enroll and
`-tags awtest` out of all evaluator paths; Docker out of the quickstart (kept in the self-host
guide); the 3-agent first policy replaced by `examples/quickstart.yaml`; no docs framework beyond
plain MDX in `web/`; no `awd demo`. This milestone adds no new flags or knobs.

## Not claimed anywhere until evidence exists
No "N minutes" claim until a timed run exists [NEED]. No customers, logos, quotes, metrics or
SOC 2. No "only" or "first". Each limit sits beside the claim it qualifies.

## Merged Top 5

| # | proposal | why it sells | effort S/M/L | depends on | in-milestone or later |
|---|---|---|---|---|---|
| 1 | Release binaries (linux/darwin, amd64/arm64, checksums) + `install.sh` to `~/.local/bin`; fix module path | Removes the clone/Go/Docker wall, today's biggest drop-off before any value is seen | M | CTO (goreleaser, repo path) | In: blocker for everything else |
| 2 | Rootless 7-step quickstart: `quickstart.yaml` (one `.env` deny), step 6 refusal, step 7 adds a repo-scoped curl deny → two directories, two outcomes; token from stdin; doctor "evaluation mode" line; ends at /demo "Bring your rules" | The evaluator sees their own rule govern their own `claude`, plus per-repo targeting, the thing vendors lack | M | 1; CTO for the two CLI fixes | In |
| 3 | Evaluate pages: "Security model and limits" (replaces the missing threat-model promise) + "Enforce on bare claude" (test VM, sudo, offline proof, cleanup) + link to `trust.md` | The security lead decides; candor and the bare-`claude` + offline proof are what get past their CISO with no customers | M | 2; CTO review of every limit statement | In |
| 4 | README ≤60 lines under one name and line ("Agentic Warden: One policy for every coding agent"): subhead, hero clip, 3-line try-it → /docs/quickstart, limits link, /demo, license; internals → `docs/contributing/` | 60-second belief; stops burying "Try it" at line 215; one name builds recall | S | 2 (link), 5 (hero) | In |
| 5 | GIF per step: vhs tapes in CI for CLI steps, 3 hand-recorded agent clips (step 6, step 7 per-repo = hero, bare `claude`) | Honors the founder's ask without GIF rot; the hero shows the mechanism, which is our only credible claim | M | 2, 3; CTO on safe agent recording | In |

Later (founder input or evidence needed): two-option pricing; dated "Compared with vendor managed
settings" page; search entry-point pages (Sharp's reach tripwire); a "did it work?" completion
signal at the end of the quickstart.
