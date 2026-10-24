# Exec room r1 — CEO: docs and onboarding as the sales motion

Date: 2026-10-24. Reader: platform or security lead evaluating a purchase (founder fact).
Sources: `.agents/product-marketing.md`, `README.md`, `web/content/*.ts`, `web/app/page.tsx`, `docs/hosting/trust.md`.

## Position summary

We have no customers, logos or metrics. The only proof we can show is the product working,
plus being candid about where it stops. So the docs are the sales motion, not a support cost.
The buyer is a security or platform lead. They do not buy from a feature list. They buy when
three things are true:
1. "It closes a gap my vendor consoles leave open." (per repo, per group, across agents)
2. "I watched it work, and it cannot break my developers." (fails safe, exits 0, offline)
3. "I can defend it in our security review." (data flow, key custody, written limits)

Today the README makes them dig for all three. It opens with a gap analysis against Anthropic and
OpenAI, puts "Try it" at line 215 with hand-built curl and JSON, and mixes contributor internals
(Codex `-c` key rules, Gemini tier quirks, env-var tables) into the pitch. It is also titled
`agent-wrapper`, not Agentic Warden.

**My bet:** a single **10-minute proof** is the centre of onboarding. One command brings up a
seeded control plane and two sample repos (payments, sandbox). In the payments repo the same
`claude` launch gets `Read(./.env)` and `Bash(curl *)` denied. In the sandbox only `.env` is
denied. `aw doctor` explains why. Everything else (README, /docs, CTA) points into or out of that
moment. The conversion action follows it: **"Bring your rules: a policy-mapping call, leading
to a hosted-cell pilot."** Self-host is the proof path and the fallback. It is not the revenue goal.

## Buyer journey

### 60 seconds (README top, site /docs landing)
They must believe: *this is a governance layer for Claude Code, Codex and Gemini CLI. It targets
rules by IdP group and by repository. It builds on the vendors' own managed settings and is never
in the model-traffic path.*
- One-line what, one-line who, then the 15-line policy YAML that is already on the homepage
  (`home.hero.policy`). That YAML is the best thing we own: a security lead can read it and see
  their own rules in it.
- A 3-row status-quo table: vendor console / MDM files / Agentic Warden × per-repo, per-group, multi-agent.
  This replaces the 20-line gap essay.
- Three trust facts in one line each: fails safe (always exits 0), signed bundles (Ed25519 or KMS),
  code and prompts never reach us (`trust.md`).
- Two buttons: "Run the 10-minute proof" and "Bring your rules (book a call)".

### 10 minutes (quickstart)
They must believe: *I saw it enforce, it explained itself, and nothing broke when I pulled the plug.*
- Steps: start (one command) → apply a policy → launch in two repos → `aw doctor` → stop the
  server and launch again (still governed, still starts). That last step is the fail-safe proof,
  and no slide can make it.
- No curl, no JSON, no `-tags awtest` visible to the reader. If the walkthrough needs the
  test build, hide it inside the demo image. [CTO to judge feasibility]
- End page: "Map your own rules" with the CTA. [NEED: instrumentation to count quickstart
  completions vs demo requests, or we cannot tell if this works]

### Before the call (security review packet)
They must believe: *I can defend this to my CISO and my auditors, and I can leave if I want to.*
- **Limits page**: lift the "Honest limits" list from the context doc verbatim. Codex and Gemini
  repo rules are advisory. Signing is detection, not prevention, against root. Repo scoping keys
  on an editable `.git/config`. claude.ai server-managed settings shadow the helper. Candor here
  is our main differentiator against a vendor's marketing page.
- **Threat model**: README says "see `docs/threat-model.md` when it lands". It has not landed.
  A security buyer will notice. Ship a short one: it is the one doc on the path to the deal
  that we do not have.
- **Architecture and data flow**: one diagram (awd → aw-sync → policyHelper) plus "what never reaches us".
- **Trust statement** (`docs/hosting/trust.md`) linked as-is; it is already good.
- **Exit / lock-in page**: Apache-2.0, plain YAML policy, vendor-native files on disk,
  `aw-sync uninstall-timer`. [NEED: verify and document what a full removal leaves on a machine]
- [NEED: SOC 2 / pen test / third-party review — say "none yet" rather than stay silent]

## Objections that kill the deal, and the doc that answers each

| Objection | Answer we can honestly give | Where |
|---|---|---|
| "Fails our security review" | Threat model, limits, trust.md, data flow, KMS + CloudTrail `Sign` events | Security packet |
| "Anthropic will ship this natively" | Vendors add per-group controls one agent at a time. Our lasting value is per-repo and cross-agent. We write into the vendors' managed tier, so their new features become inputs for us, not replacements. A weekly drift job checks our keys against upstream schemas. | FAQ + limits |
| "Lock-in / early-stage vendor" | Apache-2.0, self-host, plain YAML, native files; "Status: early" stated plainly | Exit page |
| "Will it break developer launches?" | Helper exits 0, offline, schema-validated; the quickstart's pull-the-plug step | Quickstart |
| "Another service to run" | Hosted-cell pilot | CTA |

## Proof we can show today (no customers)
- The reproducible 10-minute proof itself. A "show, don't claim" buyer is the right buyer.
- Open source with tests that need no network (`go test ./...`), and the weekly upstream drift check.
- Written limits and the trust statement. Few vendors publish theirs.
- Never: logos, "trusted by", counts, quotes. [NEED: first design-partner quote, permission-cleared]

## Council digest (simulated: built from published frameworks, not the advisors' actual views)
- **Dunford:** the real alternative is the status quo (vendor managed settings + MDM files), not
  named rivals. Frame us as "managed settings, per repo and group, for every agent", a category
  they already own. Drop the competitor essay; keep one status-quo table.
- **Hormozi:** raise the value equation. Perceived likelihood goes up with live proof; time delay
  goes down (10 min); effort goes down (one command). Make the offer hard to refuse: we translate
  your current rules into a policy and run it on a hosted cell for the pilot. [NEED: pilot terms
  and price metric]
- **Godin:** smallest viable audience is security leads at orgs running 2+ agents with repos of
  differing sensitivity. The remarkable thing is candor: a public limits page is something they repeat.
- **Sharp (dissent):** differentiation is rarely perceived, and a 10-minute demo only reaches people
  who already found us. Without mental availability (showing up when a lead searches "Claude Code
  managed settings per repository") the funnel is empty. Docs pages on those entry points matter
  more than polish.
- **Chair:** the proof-first quickstart wins for this stage. Keep Sharp as a tripwire: if the quickstart
  ships and traffic stays flat, the problem is reach, not docs. Answer it with 3–4 entry-point
  pages, not more GIFs.

## What to cut
- **GIF per step.** This is vanity: every CLI change breaks a GIF, and security readers copy commands,
  they do not watch them. Make **one** recording of the aha moment (two repos, two outcomes) for
  README/home; use copyable text everywhere else.
- **The competitor gap analysis at the top of the README.** Becomes one table. The long form moves to /docs "Compared with vendor controls".
- **Contributor internals in README** (Codex `-c` key rules, Gemini tier mechanics, signing protocol
  headers, `internal/` layout, env-var table, site dev, drift maintenance) → `docs/` and `CONTRIBUTING`.
- **The from-source "Try it" path in README** (curl, JSON, enroll by hand) → contributor docs.
- **Three pricing tiers that all say "Contact us"**, one of them "Coming soon". Two paths read more
  honestly: "Self-host (Apache-2.0)" and "Hosted pilot (design partners)". [Judgement call: founder to confirm]
- **A heavy docs framework** (versioning, search, i18n) before we have readers. Plain MDX pages in `web/` are enough.
- Rename the README title to Agentic Warden.

## Top 5 proposals

| # | proposal | why it sells | effort guess S/M/L | depends on |
|---|---|---|---|---|
| 1 | 10-minute proof quickstart: one command, seeded policy, payments vs sandbox repo, `aw doctor`, pull-the-plug step; one recording of the aha | Turns "trust us" into "I saw it" with zero customers. Proves both the gap and the fail-safe | M–L | CTO: demo image/seed without `-tags awtest` exposed; Claude Code needed locally or simulated output |
| 2 | Security review packet in /docs: threat model (missing today), limits page, data-flow diagram, trust.md, exit/uninstall page | The security lead is the decision maker; this is what gets past their CISO. Candor is our differentiator | M | Threat model authoring (CTO + security); verify uninstall behaviour |
| 3 | 60-line README: what/who, policy YAML, status-quo table, 3 trust facts, two CTAs; internals moved to docs/ | 60-second belief. Stops burying "Try it" at line 215 | S | Proposal 1 for the quickstart link |
| 4 | Single conversion path "Bring your rules" → policy-mapping call → hosted-cell pilot; simplify pricing to self-host vs hosted pilot | One clear next step that matches the business goal (design partners on hosted cells) | S | Founder: pilot terms, pricing metric [NEED]; CMO copy |
| 5 | 3–4 entry-point docs pages ("per-repository rules for Claude Code", "one policy for Claude Code, Codex and Gemini", "Claude Code managed settings by IdP group") | Sharp's tripwire: we can only convert leads who find us; docs are our cheapest reach | S–M | CMO/SEO; content from 2 and 3 |
