# Exec room r2 · CMO · Revised after the CEO and CPO papers

Inputs: r1-ceo.md, r1-cpo.md, r1-cmo.md, the CPO's verified facts (the rootless chain works today with `aw-sync enroll`; there are no release binaries; the module path `github.com/acme/agent-wrapper` breaks `go install`; agent GIFs need a logged-in Claude).

## 1. What I changed and why

- **Dropped my "one-command enroll" dependency.** `aw-sync enroll` already does the job, and the README never used it. The CPO's stdin piping is a nicety, so the README draft below uses the flag form that works today.
- **Hero GIF now shows quickstart step 6 (`aw claude` refuses to read `.env`) instead of plain `claude` in payments vs sandbox.** The hero has to show something the reader can reproduce in 10 minutes without root. The plain-`claude` and offline proof (the CPO's "aha+") moves to its own hand-recorded clip on "Enforce on plain `claude`", linked from the README.
- **Kept the differentiator in the quickstart.** I propose one change to the CPO's step 7: the new curl deny is scoped to `repos: ["*payments*"]` (or similar), and the reader asks twice, once in `demo-repo` and once in a second folder. That puts the CEO's "two repos, two outcomes" inside the CPO's loop without adding a step. [NEED: CPO/CTO check that a local demo repo with a matching origin is cheap to set up in step 0]
- **Renamed "Evaluate" to "Security review"**, the buyer's own word. It is the CEO's packet (limits, threat model, trust statement, data flow, removal) and sits second in the nav, after Get started.
- **One CTA, using the site's existing words:** "Bring your rules", which leads to /demo. It covers the CEO's policy-mapping call and the CPO's "Request a sandbox cell". There is no "Sign up", because the hosted console is not live.
- **Status-quo table in the README; named-vendor comparison later.** See (c).
- **I endorse the CEO's two-path pricing** (Self-host under Apache-2.0, Hosted pilot). It's a founder call, so it is not in my Top 5.

### (a) GIFs: one rule
Every quickstart step gets a GIF with the copyable command above it, which keeps the founder's ask. Steps that only run the CLI (1–5, 7) are **vhs tapes run in CI**, so a CLI change re-renders them instead of breaking them (this answers the CEO's maintenance objection). **Two agent clips are hand-recorded** because they need a logged-in Claude: the hero/step 6, and "Enforce on plain `claude`". The denial text must come from a real capture [NEED: exact wording from a real run]. The CEO gets one recording at the top, the CPO gets the split, and the founder gets a GIF per step.

### (c) Competitor page: later
**In this milestone:** a 3-row status-quo table in the README and on /docs/how-it-works. It compares mechanisms only (vendor console, MDM settings files, Agentic Warden), and every cell is already stated in today's README, dated "as of October 2026". **Later:** the long "Compared with vendor controls" page that names Anthropic's gateway and OpenAI Agent Security. It waits for a founder-confirmed scan, because a wrong claim about a vendor costs us more with a security lead than a missing page does.

### (d) Inconsistency fixes, one line each
1. Use **Agentic Warden** in the README H1, all prose and `site.docsUrl`. Change the Go module path to `github.com/alchristleo/agentic-warden` (this also fixes `go install`).
2. Delete the README status sentence "Claude Code is the only agent `aw` launches", because `aw codex` and `aw gemini` ship.
3. Console status everywhere: "Admin console: available self-hosted; hosted console coming soon."
4. Pair the site's signing claim ("a tampered bundle never reaches an agent") with "detection, not prevention, against root", and link /docs/security-model.
5. Write `docs/threat-model.md` (Security review) or remove the README's promise of it.

## 2. README top (final draft, the first ~45 of about 60 lines)

```markdown
# Agentic Warden

**One policy for every coding agent.**

Write your rules once. Agentic Warden delivers them to Claude Code, Codex and Gemini CLI
on every developer machine, scoped by identity-provider group and by repository.
Claude Code applies them at launch, even when a developer types plain `claude`.

![Claude Code refuses to read .env because the policy denies it](docs/assets/hero.gif)

Status: early. Self-host today; hosted cells for design partners.

    rules:
      - name: baseline
        agents: { claude: { managed: { permissions: { deny: ["Read(./.env)"] } } } }
      - name: payments-repos
        match: { repos: ["github.com/acme/payments*"] }
        agents: { claude: { managed: { permissions: { deny: ["Bash(curl *)"] } } } }

| As of October 2026            | Per repository | Per IdP group      | Claude Code, Codex, Gemini CLI |
|-------------------------------|----------------|--------------------|--------------------------------|
| Vendor admin console          | No             | Varies by vendor   | One vendor each                |
| Managed settings files (MDM)  | No             | No                 | One format per vendor          |
| Agentic Warden                | Yes            | Yes                | One policy                     |

- **Fails safe.** The launch helper works offline from the last synced bundle and never blocks a developer.
- **Signed bundles.** Each machine verifies an Ed25519 or AWS KMS signature before it writes anything.
- **Out of the model path.** Your code and prompts never reach the control plane ([trust statement](docs/hosting/trust.md)).

## Try it (about 10 minutes, no root)

    export AWD_ADMIN_TOKEN=try-me; awd serve &
    awd apply examples/quickstart.yaml
    AW_SYNC_TOKEN=$(awd enroll-token you@acme.com) aw-sync enroll --server http://localhost:8080 --agents claude --state-dir ~/.aw-try
    aw-sync once --state-dir ~/.aw-try
    cd demo-repo && AW_SYNC_STATE_DIR=~/.aw-try aw claude "read .env"

[Quickstart](<site>/docs/quickstart) · [Enforce on plain `claude`](<site>/docs/enforce-plain-claude) ·
[Security model and limits](<site>/docs/security-model) · [Bring your rules](<site>/demo) · Apache-2.0
```

Notes. The commands come from r1-cpo.md, using the flag form of `aw-sync enroll` that works today. Getting the binaries is not shown: until there are releases and `install.sh`, the line above them is "clone, then `go build ./cmd/...`". [NEED: CTO, release binaries; `examples/quickstart.yaml` does not exist yet]. The "Varies by vendor" cell is a deliberate summary of what today's README says (Codex has per-group rules on ChatGPT plans; Claude seats do not). The table must not get more specific without the scan. The YAML is the homepage policy, compacted. Headline alternatives B and C from r1 stay parked for ads.

## 3. /docs landing (final copy)

- **H1:** Agentic Warden docs
- **Lede:** See your own rule stop your own agent in about 10 minutes. Then read what it enforces, where it stops, and how to roll it out.
- **Primary CTA:** Start the quickstart · **Secondary:** Bring your rules
- **Prerequisites line:** Linux or macOS, Claude Code installed and logged in. Docker is not needed.
- **Three cards**
  - *Get started*: the 7-step quickstart, then the same rule enforced on plain `claude` on a test VM.
  - *Security review*: what it enforces, what it does not stop, who holds the keys, and how to remove it.
  - *Roll out*: machines on Linux, macOS and Windows, groups from Okta or Entra ID, keys in AWS KMS.

Nav:
1. **Get started:** Overview · Quickstart · Enforce on plain `claude` · How it works (includes the status-quo table)
2. **Security review:** Security model and limits · Threat model [NEED: write it] · Trust statement (hosted cells) · Removing Agentic Warden [NEED: verify what removal leaves on a machine]
3. **Guides:** Write your first policy · Per-repository rules · Groups and your IdP · Roll out to a fleet · Codex and Gemini CLI · Sign bundles · Admin console · Self-host for production
4. **Reference:** `awd` · `aw-sync` · `aw` · `aw-policy` · Environment · Policy schema · Files written per OS · HTTP API

Contributor internals go to the repo's `docs/contributing/`, outside /docs. The closing band on Security review pages keeps the existing site line: "See it against your own policy. Bring the rules you enforce today."

## 4. Quickstart GIF storyboards (6 s or less each; CPO's 7 steps)

1. Install (scripted): the installer prints four binaries and their versions. Until binaries ship, this is `go build`.
2. Serve (scripted): `awd serve &` prints `listening on :8080`.
3. Apply (scripted): `examples/quickstart.yaml` shows one deny line, and `awd apply` reports one rule applied.
4. Enroll (scripted): `aw-sync enroll` prints the machine ID.
5. Sync (scripted): `aw-sync once` reports the bundle synced.
6. Aha (hand-recorded, also the hero): `aw claude "read .env"` and Claude refuses, citing the policy.
7. Make it yours (scripted for the CLI part, hand-recorded for the refusals): add the repo-scoped curl deny, apply, sync, then curl is refused in the payments folder and runs in the other one.
- Aha+ (hand-recorded, on Enforce on plain `claude`): plain `claude` is refused, `awd` is stopped, and plain `claude` is still refused.

## 5. Merged Top 5

| # | proposal | why it sells | effort S/M/L | depends on | in-milestone or later |
|---|---|---|---|---|---|
| 1 | One name everywhere: Agentic Warden in the README H1, prose, docsUrl and Go module path; one line, "One policy for every coding agent" | A buyer can't shortlist a product they can't name; the module fix also unblocks `go install` | S | Founder sign-off; CTO on the module rename | In |
| 2 | README of about 60 lines (draft above): headline, hero GIF, YAML, dated status-quo table, 3 trust facts, real try-it block, one CTA "Bring your rules" | 60-second belief for a security lead, with "Try it" moved from line 215 to the first screen | S–M | #3 for the hero and install line; `examples/quickstart.yaml` | In |
| 3 | CPO's rootless 7-step quickstart with a GIF per step: CLI steps as vhs tapes in CI, agent clips hand-recorded; step 7 adds a repo-scoped rule | The evaluator watches their own rule stop their own Claude, and sees per-repo targeting, the thing vendor consoles lack | M–L | Release binaries + install.sh (CTO); `quickstart.yaml`; a real denial capture; doctor's "evaluation mode" line | In |
| 4 | Security review section, second in the nav: security model and limits, threat model, trust statement, Enforce on plain `claude`, Removing it | This reader buys on stated limits and offline enforcement, and candor is our proof while we have no customers | M | Threat model authoring (CTO); removal behaviour verified | In (threat model can ship last) |
| 5 | Named-vendor "Compared with vendor controls" page plus 3–4 entry-point pages (e.g. "per-repository rules for Claude Code") | Reach: we can only convert leads who find us (Sharp's tripwire) | S–M | Founder-confirmed competitor scan [NEED]; pages from #2–#4 | Later |
