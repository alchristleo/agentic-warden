# Exec room r2: CPO, revised after the CEO and CMO papers

I read r1-ceo.md and r1-cmo.md. On 2026-10-24 I ran the revised path below up to `aw doctor`.
I did not launch Claude. Each claim says what I verified.

## What changed from r1, and why

1. **Two repos, payments and sandbox (CEO, CMO).** In r1 I used one rule. Two repos show the
   gap that vendor consoles leave open, per repository, and they cost one visible line:
   `for r in payments-api sandbox; do git init -q $r && git -C $r remote add origin https://github.com/acme/$r; done`.
   Verified: `aw doctor` in `payments-api` reports `policy compiled for github.com/acme/payments-api`.
2. **A "pull the plug" step (CEO).** `aw` compiles from the bundle on local disk
   (`resolvePolicy` reads only the state directory), so stopping `awd` changes nothing. This is the
   fail-safe proof. `[NEED: record it with awd stopped and a real Claude session]`
3. **I dropped "aw-sync enroll reads stdin".** `AW_SYNC_TOKEN=$(awd enroll-token …) aw-sync enroll …`
   already works as one line (verified). The README's curl enroll was never needed.
4. **Correction to r1:** a rootless `aw-sync once` needs `--root claude=DIR`. Without it, it tries
   `/etc/claude-code` and fails (verified). The quickstart shows the flag and says why: it keeps files out of /etc.
5. **The binaries are not a blocker for this milestone.** `go build -o bin/ ./cmd/...` builds all 4
   binaries in one command (verified). The quickstart starts with clone and build, and swaps to the
   installer when releases exist. The module-path fix only matters for `go install`, so it moves to later.

## Rulings on the conflicts

**(a) GIFs: one per step, as the founder asked, but generated. Plus one hand-recorded agent clip.**
The CEO's objection is rot: every CLI change breaks a GIF. Rot comes from hand-made GIFs, so the
CLI steps become `vhs` tapes that are rendered again in CI. A changed output then produces a new
GIF, not a stale one. The CMO's 5–6 GIFs match the step list below. Only the Claude session
cannot be scripted, because it needs a login and a live model. So **one** real recording covers
it: the payments vs sandbox refusal. It is used as the hero and again at step 6. This keeps the
founder's "GIF per step", answers the CEO's maintenance cost, and gives the CMO his hero.
The CMO's hero shows **plain `claude`** refusing. The rootless quickstart uses `aw claude`, so the
hero is recorded on the test-VM path ("Enforce on bare claude"), and its caption says so.
Showing plain `claude` while the quickstart does something weaker would be the first
broken promise a security reader finds. `[NEED: CTO, whether a real login may appear in a public GIF]`

**(b) No `make demo`. Seven visible steps are the proof.**
The reader has to trust two things: that rules they wrote reach a machine they enrolled, and that
the agent obeys. One command that seeds everything proves that we can seed a demo, and nothing
more. It also hides the four commands they will run on real machines, and gives an "it's a
staged demo" objection for free. Seven short commands, about 10 minutes, keep the "you set it up
yourself" proof `[NEED: timed run by someone new before we print "10 minutes"]`.
The CEO's goal (no curl, no JSON, no `-tags awtest`) is met without hiding anything: none of the
three appears.

**(c) IA: four sections, landing cards from the CMO, Evaluate second.**
The CMO's seven sections are too many for a docs site with about 20 pages. Write policy, Roll out
and Operate merge into Guides. Contributing leaves the site for the repo. The quickstart stays
first because it is the primary CTA and the aha. Evaluate comes next, and every quickstart step
links its "why" into Evaluate.

**(d) The prerequisite split is in the table below.** Release binaries, `install.sh` and the module
path become a separate follow-up. They are not blocking, but the founder should know they cost
us readers: a security lead without Go stops at step 1. `[NEED: share of evaluators without a Go toolchain]`

## Final quickstart (rootless, nothing written to /etc)

Prerequisites: Linux or macOS, Go 1.24+, git, Claude Code installed and logged in.

| # | Command | You should see | GIF |
|---|---|---|---|
| 1 | `git clone … && cd agentic-warden && go build -o bin/ ./cmd/... && export PATH=$PWD/bin:$PATH` | `aw awd aw-policy aw-sync` in bin/ | tape |
| 2 | `export AWD_ADMIN_TOKEN=try-me; awd serve &` | awd listening (in memory, evaluation only) | tape |
| 3 | `awd apply examples/quickstart.yaml` (deny `Read(./.env)` everywhere, deny `Bash(curl *)` in `github.com/acme/payments*`) | `applied revision … (2 rules)` | tape |
| 4 | `export AW_SYNC_STATE_DIR=~/.aw-try`, then `AW_SYNC_TOKEN=$(awd enroll-token you@acme.com) aw-sync enroll --server http://localhost:8080 --agents claude --state-dir ~/.aw-try && aw-sync once --state-dir ~/.aw-try --root claude=$HOME/.aw-try/claude` | `enrolled machine …`, `synced bundle …` | tape |
| 5 | The two-repo line from change 1, then `cd payments-api && aw doctor` | `policy compiled for github.com/acme/payments-api`, the curl deny | tape |
| 6 | `aw claude` in `payments-api`, ask it to curl: refused. Same in `sandbox`: allowed | **aha** | recorded |
| 7 | Edit the YAML, `awd apply`, `aw-sync once …`: the new rule takes effect. Then stop `awd` and repeat step 6: same result (in-memory `awd` loses state, so the plug is pulled last) | the policy is *theirs*, and it fails safe | tape + recorded |

Then: "Enforce on bare `claude`" (test VM, sudo). This page is the hero clip, the purchase trigger, and the undo steps.
Small product items: `examples/quickstart.yaml`, and an `aw doctor` "evaluation mode" line in place
of the 6 rootless warnings (still true, still in `--json`). Without that line, step 5 opens on a
red screen. Steps 5–7 need `AW_SYNC_STATE_DIR`, so step 4 exports it once.

## Final IA (`/docs` on web/)

Landing: CMO lede and three cards (Evaluate / Try it / Roll out). Primary CTA: quickstart.
Secondary: Request a demo.

- **Get started**: Overview (what it governs, a 3-part diagram) · Quickstart · Enforce on bare `claude` (VM, offline, undo)
- **Evaluate**: How enforcement works · What it does not stop (limits, verbatim from context) ·
  Security and trust (signing, KMS, trust.md) · Threat model `[NEED: missing, README promises it]` ·
  Compared with vendor managed settings (dated)
- **Guides**: Write your first policy · Per-repository rules · Groups and your IdP (snapshot, SCIM) ·
  Codex and Gemini CLI (enforced vs advisory) · Roll out to a fleet (MDM, timer, `awd machines`, revoke) ·
  Sign bundles and rotate keys · Admin console (self-hosted) · When the control plane is down
- **Reference**: CLI (`awd`, `aw-sync`, `aw`, `aw-policy`, generated from `--help`) · Environment variables · Policy schema · Files written per OS · HTTP API
- **Out of the site**: README is 60 lines or fewer (CMO draft, with step 1 as above). `docs/contributing/` holds layout, tests, the drift job, web/ dev and internals.

## Merged Top 5

| # | proposal | why it sells | effort S/M/L | depends on | in-milestone or later |
|---|---|---|---|---|---|
| 1 | 7-step rootless quickstart: two repos, `aw doctor`, a real refusal, pull the plug, edit and reapply. Ships `examples/quickstart.yaml` and the doctor "evaluation mode" line | The evaluator sets it up themselves and watches their own rule block their own agent, then survive an outage. This is proof without customers | M | timed run; one real Claude recording | in-milestone |
| 2 | Evaluate track: How it works, What it does not stop, Security and trust, Threat model, dated vendor comparison | Gets the decision maker past their own security review. Candour is our credibility | M | threat model authoring (CTO + security) | in-milestone (threat model may trail) |
| 3 | One name and one line, plus a 60-line README with the hero clip and links into /docs; internals move to `docs/contributing/` | First screen shows value and is remembered; the buyer stops wading through internals | S | 1 for the hero clip; founder sign-off on the name | in-milestone |
| 4 | GIFs: `vhs` tapes per CLI step rendered in CI, plus one real recording of plain `claude` refusing (VM path) used as hero and at step 6 | Keeps the founder's per-step GIFs without rot; the hero shows the mechanism honestly | M | 1; CTO on the recording harness and login exposure | in-milestone |
| 5 | Release binaries, `install.sh`, module path matching the repo | Removes the Go prerequisite, the likeliest drop-off for a security lead | M | none | later (follow-up, swap step 1) |

The CEO's conversion path ("Bring your rules" → hosted pilot) and the CMO's entry-point pages are
real, but they are copy and pricing work rather than onboarding. I defer to them on both, and each
quickstart page ends with that CTA.
