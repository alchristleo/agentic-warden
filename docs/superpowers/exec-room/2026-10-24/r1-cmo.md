# Exec room r1 · CMO · Docs and onboarding that sell

Reader: a platform or security lead evaluating a purchase. Sources: `.agents/product-marketing.md` (its [NEED] gaps stand), README.md, `web/content/*.ts`, `web/app/page.tsx`.

## 1. Council digest (simulated: built from published frameworks, not the advisors' real views)

Seated: Dunford, Schwartz, Ogilvy; Sharp as dissenter.

- **Dunford (alternatives first).** Without us, this buyer keeps the status quo: each vendor's managed settings pushed by MDM, plus each vendor's console. That is the alternative to name. Inventing a new category ("agent governance") means selling the problem and the solution together. Stand in a frame the buyer already budgets for, managed settings / policy for developer tools, and be the big fish in one small pond: **per-group and per-repository rules across more than one agent**. Today the README opens with a vendor gap analysis. That walk through the alternatives belongs in the pitch, but after the claim, on its own page.
- **Schwartz (awareness and sophistication).** In late 2026 this reader is *solution-aware*. They know agents run shell commands in their repos, and they know vendor managed settings exist. They are not yet product-aware of us. The market for "secure your AI agents" claims looks like stage 3: the plain claim is worn out [NEED: competitor scan to confirm]. So lead with the **mechanism**: rules are written into each agent's own managed tier, and Claude Code applies them at launch even on bare `claude`. The hero GIF has to show that mechanism working, because adjectives won't carry it.
- **Ogilvy (research, fact, headline).** A security lead reads like an auditor: give facts, file paths and limits. The candid limits (advisory Codex/Gemini repo rules, detection rather than prevention against root) are the most persuasive copy we own, because no one trusts a control that claims to have no gaps. Spend the headline on one specific promise and put a demo request on every evaluation page.
- **Sharp (dissent).** With no customers, differentiation claims are weak evidence. Buyers rarely notice feature deltas. What they need is to remember us when the buying moment comes ("a team just adopted Codex", "audit asked who controls agent permissions"). Today we use two names (`agent-wrapper` and Agentic Warden) and two headlines, so we are building no memory. Pick one name, one line and one recurring visual (the YAML policy next to a denied `claude`), and use them everywhere for years.

**Disagreements.** (1) Dunford against Sharp, a sharp niche against broad memorability. Trade-off: early on, a sharp niche wins demos, and distinctiveness compounds later. Both win if the niche line *is* the distinctive asset. Settle it with demo-form answers to "what made you ask?" [NEED: add that field]. (2) Schwartz against Ogilvy, mechanism-first against benefit-first headline. Answer: the headline carries the benefit, and the subhead plus the GIF carry the mechanism.

**Chair.** Category: control plane for AI coding agents, framed as managed settings per group and per repo. Alternative: per-vendor managed settings plus MDM. Stage: solution-aware, sophisticated market, so lead with the mechanism and earn trust with the limits.

## 2. Positioning statement

For platform and security leads whose developers run Claude Code, Codex and Gemini CLI, Agentic Warden is a control plane for AI coding agents. It turns one policy into each agent's own managed settings, scoped by identity-provider group and by repository. Today's alternative is per-vendor managed settings delivered by MDM, which apply machine-wide, one vendor at a time. Agentic Warden adds rules per repository and per group in a single policy. Claude Code applies that policy at every launch with no wrapper, and machines verify a signed bundle before they write anything.

## 3. README top (draft)

Diagnosis: B. The site already has a voice (precise, candid, plain), so this draft keeps that voice and fixes the order of the README.

```markdown
# Agentic Warden

**One policy for every coding agent.**

Write your rules once. Agentic Warden delivers them to Claude Code, Codex and Gemini CLI
on every developer machine, scoped by identity-provider group and by repository.
Claude Code applies them at launch, even when a developer types plain `claude`.

![A policy denies curl in the payments repo; plain claude obeys it](docs/assets/hero.gif)

## Try it

    docker compose -f deploy/docker-compose.yml up -d    # control plane on :8080
    awd apply examples/org-policy.yaml                   # your rules
    aw doctor                                            # what this machine enforces, per repo

[Quickstart](https://<site>/docs/quickstart) · [How enforcement works](https://<site>/docs/how-it-works) ·
[What it does not stop](https://<site>/docs/limits) · [Request a demo](https://<site>/demo) · Apache-2.0
```

Status line under the subhead, one line: "Early. Self-host today; hosted cells for design partners." Delete the stale sentence "Claude Code is the only agent `aw` launches", because `aw codex` and `aw gemini` exist.

**Headline options**
- A (chosen): **One policy for every coding agent.** This is already the site H1. Reusing it gives Sharp the consistency he asks for. It passes the swap test only together with the subhead, so the subhead must name the three agents and "per repository".
- B: **Different agent rules for your payments repo and your sandbox.** The reader can picture it, and it is the sharpest form of the Dunford niche. It is better as a /docs or ad headline.
- C: **Rules your developers can't skip by typing plain `claude`.** This is the mechanism-first headline Schwartz would pick. Risk: it invites "can they bypass it?". The honest answer is "not without local admin" and Codex/Gemini differ, so it must link to /docs/limits.

**Hero GIF storyboard (about 12 s, looped, one terminal split into two panes)**
1. (0–2 s) Left pane, titled "platform team": `org-policy.yaml` with two rules: deny `Read(./.env)` everywhere, deny `Bash(curl *)` in `github.com/acme/payments*`.
2. (2–4 s) Left: `awd apply org-policy.yaml` prints that the policy was accepted. Right pane, titled "alice's laptop": `aw-sync` cycle line.
3. (4–8 s) Right: `cd payments-api` then plain `claude`. Alice asks it to curl an endpoint, and Claude Code refuses, citing managed policy.
4. (8–11 s) Right: `cd ../sandbox`, same request, curl runs.
5. (11–12 s) Caption: "Same developer, same agent, different repo." Then cut to the logo.
Dependency: a scripted, reproducible recording with a real Claude Code session [NEED: CTO, a recording harness, and whether a real login can appear in a public GIF]. Fallback: steps 3–4 use `aw doctor` output instead of a live session. It is weaker, but deterministic.

## 4. /docs landing (draft)

Above the fold:
- **H1:** Agentic Warden docs
- **Lede:** Evaluate it against your own rules, try it on one machine, then roll it out to the fleet. Every page says what the control enforces and where it stops.
- **Primary CTA:** Start the quickstart · **Secondary:** Request a demo
- **Three cards**
  - *Evaluate*: how a rule gets from your policy to the agent, what it does not stop, keys and trust.
  - *Try it*: run the control plane locally and govern your own `claude` in [NEED: timed run, e.g. "10 minutes"].
  - *Roll out*: enroll machines on Linux, macOS and Windows, connect Okta or Entra ID, move keys to AWS KMS.

Section headers (sidebar order follows the buyer's path):
1. **Evaluate** – How enforcement works · What it does not stop · Compared with vendor managed settings · Security and trust (signing, KMS, hosted-cell trust statement) · Threat model [NEED: `docs/threat-model.md` is promised in README, does not exist]
2. **Quickstart** – five steps, one GIF each (below)
3. **Write policy** – Rules, groups and repo matching · Precedence · Claude Code · Codex · Gemini CLI
4. **Roll out** – Enroll machines · Scheduled sync per OS · SCIM (Okta, Entra ID) · Signing keys and rotation · Admin console (self-hosted)
5. **Operate** – `aw doctor` · When the control plane is down · Revoking a machine
6. **Reference** – Policy schema · CLI (`awd`, `aw-sync`, `aw`) · HTTP API
7. **Contributing** – internals moved from README (layout, tests, drift job, marketing site)

Closing band on every Evaluate page: "See it against your own policy. Bring the rules you enforce today." → Request demo (existing site line).

## 5. Quickstart GIF storyboards (6 s or less each)

1. **Start the control plane.** `docker compose up -d`, a health line shows `awd` listening on :8080.
2. **Apply a policy.** Editor shows the two-rule YAML; `awd apply` returns success; `awd groups` lists `platform`.
3. **Enroll this machine.** One enroll command prints a machine ID; `aw-sync once` lists the files it wrote. [NEED: CTO, a one-command local enroll to replace the curl + JSON in README "Try it"]
4. **See the rules per repo.** `aw doctor` in `payments-api` shows the curl deny; the same command in `sandbox` does not.
5. **Launch plain `claude`.** In `payments-api`, a curl request is refused by managed policy; no `aw` prefix was typed.
6. **Pull the plug.** Stop `awd`, launch `claude` again: same rules, from the last synced bundle. (Optional, signing on: tamper with the bundle and `aw-sync once` refuses to write.)

## 6. What to cut

- **README opening with the competitor gap analysis.** Move it to a dated "Compared with vendor managed settings" page, so the claims about Anthropic and OpenAI carry an "as of" date and don't go stale on the front page.
- **The 60-line "How policy reaches the agent" block** (Codex `-c` key character rules, Gemini tier quirks). This is reference material, so it goes under Write policy / Codex and Gemini.
- **Hand-made curl + JSON enrollment** in "Try it". Replace it with one command, or keep it only in the HTTP API reference.
- **Tests, Marketing site, Maintenance sections.** Move them to Contributing.
- **"Enterprise control plane"** in README line 1. "Enterprise" says nothing and fails the swap test. Use "control plane for AI coding agents".
- **"Contact us" price on the Self-hosted plan.** The source is Apache-2.0, so say "Free (Apache-2.0)" and keep "Talk to us" for rollout help. Gating free code behind a call costs trust with this reader.

## 7. Site vs README inconsistencies

| # | Inconsistency | Fix |
|---|---|---|
| 1 | Name: README H1 `agent-wrapper`; site, GitHub repo and metadata say Agentic Warden. A file is still named `50-agent-wrapper.toml` | Agentic Warden in all prose. Binary names stay. Rename the file only if the CTO says it's safe |
| 2 | README status says `aw` launches Claude Code only; README body and site describe `aw codex` / `aw gemini` | Delete the stale sentence |
| 3 | README says "a client-side control, not a security boundary"; the site never says it, and the product page says "a tampered bundle never reaches an agent" | Put the limits on /docs/limits and link them from the site. Keep the site's signing claim only together with "detection, not prevention, against root" |
| 4 | Site says the hosted console is "Coming soon"; README documents a working self-hosted admin console at `/console/` | Docs and site both say: console available self-hosted, hosted console coming soon |
| 5 | `site.docsUrl` points to the GitHub README | Point it to /docs once that exists |
| 6 | README promises `docs/threat-model.md`, which does not exist | Write it (Evaluate track) or drop the promise |
| 7 | Headlines differ: README "Enterprise control plane for coding agents"; site "One policy for every coding agent" | Use the site line everywhere |

## 8. Claims needing evidence before use

[NEED: design-partner quote or logo] · [NEED: time-to-first-governed-session, measured] · [NEED: competitor scan, for the "only" or "first" claims we deliberately avoid] · [NEED: SOC 2 / pen test status for the Evaluate track] · [NEED: pricing metric]. Until these exist, the docs sell through the mechanism, the GIFs and the limits.

## 9. Top 5 proposals

| # | proposal | why it sells | effort guess S/M/L | depends on |
|---|---|---|---|---|
| 1 | One name and one line everywhere: "Agentic Warden: One policy for every coding agent" (README H1, docs, site metadata, docsUrl) | A buyer can only shortlist a product they remember; two names split recall (Sharp) | S | Founder sign-off; CTO on the `50-agent-wrapper.toml` filename |
| 2 | New README top: headline, subhead, hero GIF of plain `claude` refused in payments and allowed in sandbox, 3 commands, 4 links | Shows the mechanism, which is the only credible claim in a crowded "AI security" market (Schwartz) | M | Recording harness with a real Claude session (CTO); #3 for the commands |
| 3 | 3-command quickstart, ending in 5 GIF steps; replace curl + JSON enroll with one command | A lead who governs their own `claude` in minutes has a pilot story to take to their boss | M–L | CTO: one-command local enroll / dev mode; a timed run to back any "N minutes" claim |
| 4 | /docs "Evaluate" track first: How enforcement works, What it does not stop, Security and trust, Threat model | Security leads buy on stated limits; candor is our proof while we have no customers (Ogilvy) | M | `docs/threat-model.md` (missing); `docs/hosting/trust.md` exists |
| 5 | Dated "Compared with vendor managed settings" page replacing the README gap analysis | Walks the buyer through the status quo they would otherwise keep, and why per-repo and multi-agent rules need us (Dunford) | S | Founder-confirmed competitor scan [NEED]; refresh date on the page |
