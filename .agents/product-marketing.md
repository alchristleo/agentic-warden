# Product Marketing Context

**Document version:** v1
**Last updated:** 2026-10-24
**Status:** Auto-drafted from README.md, web/content (home, product, pricing), web/app and docs/superpowers/specs. For review. Anything marked [NEED: ...] has no evidence in the repo.

## Product Overview
**One-liner:** One policy for every coding agent (site hero). Draft long form: Agentic Warden lets a platform or security team write its AI coding-agent rules once and enforce the right slice on every developer, repository and agent.
**What it does:** An organization authors one policy. A control plane (`awd`) builds a signed bundle per enrolled machine, a root-owned sync agent (`aw-sync`) writes each agent's managed settings, and Claude Code applies the rules at every launch through `policyHelper`, even when a developer types bare `claude`. Rules target identity-provider groups and repository globs.
**Product category:** Governance / control plane for AI coding agents (Claude Code, Codex, Gemini CLI). Adjacent shelves: managed settings and MDM for developer tools, AI security and policy.
**Product type:** B2B infrastructure software: control plane plus endpoint agent. Open source (Apache-2.0), self-host or hosted cell. Admin console is browser-based.
**Business model:** Three plans, all "Contact us" with no public prices: Team (hosted, badge "Coming soon"), Enterprise (adds SCIM, rollout help, self-hosted today), Self-hosted (Apache-2.0 source, PostgreSQL). Hosted cells are a manual-onboarding pilot with design partners, no billing yet (hosted-cells spec). Conversion path is "Request demo".
[NEED: actual pricing metric (per seat, per machine, flat) and price points]

## Target Audience
**Target companies:** Organizations where several teams use AI coding agents and security or platform owns developer-tool controls; likely regulated or IP-sensitive engineering orgs with an IdP (Okta or Microsoft Entra ID) and repos of differing sensitivity (e.g. payments vs sandbox).
[NEED: confirmed target size band, industries and stage; the repo names none]
**Decision-makers:** Platform engineering lead, security lead (founder-confirmed: the buyer and primary reader of docs). Developers are the governed users, not the buyer.
**Primary use case:** Enforce different agent rules per group and per repository, across more than one agent, without relying on developers to use a wrapper.
**Jobs to be done:**
- Prove to security and audit that agent permissions are centrally set and tamper-evident.
- Give high-risk repos stricter rules than the rest without a second tool per agent.
- Roll out one policy from the IdP's groups instead of hand-maintaining settings files per vendor.
**Use cases:**
- Deny reads of `.env` everywhere, deny `curl` only in payments repos.
- Roll out policy to a fleet via SCIM-provisioned groups.
- Cut over key custody to AWS KMS and watch rotation finish with `awd machines`.
- Show an auditor the compiled policy per machine (`aw doctor`).

## Personas
| Persona | Cares about | Challenge | Value we promise |
|---------|-------------|-----------|------------------|
| Decision maker: security lead | Auditable control, tamper evidence, key custody, honest limits | Vendor consoles are org-uniform; no per-repo rules; three agents, three formats | One policy, signed bundles, KMS-held keys, written trust statement for hosted cells |
| Decision maker / champion: platform lead | Fleet rollout, no developer friction, ops burden | Cross-OS delivery, outage behavior, another service to run | Timer-based sync on Linux, macOS, Windows; offline, fails safe; hosted option removes ops |
| Technical influencer: IAM / IdP admin | SCIM, group mapping | Keeping policy groups in step with the IdP | SCIM 2.0 for Okta and Entra ID; provisioned groups join authored ones |
| Financial buyer | Cost vs risk | No known budget line for agent governance | [NEED: pricing, ROI framing, budget owner] |
| User: developer (governed) | Not being blocked | Surprise denials, broken launches | Keeps typing `claude`; helper always exits 0; never blocks on outage |

## Problems & Pain Points
**Core problem:** Claude Code, Codex and Gemini CLI each ship their own settings format and their own admin controls. Organizations cannot express "this group, in this repo, for any agent" in one place.
**Why alternatives fall short:**
- Claude Code managed settings and the claude.ai console are organization-uniform; they cannot target a group or repository.
- Anthropic's self-hosted gateway gives per-group settings but needs an upstream API or cloud credential, not claude.ai seats.
- OpenAI Agent Security covers Codex on ChatGPT plans per group, but not per repository or API-key use, and not other agents.
- Hand-written settings files and MDM are machine-wide and per vendor.
**What it costs them:** [NEED: customer evidence of audit findings, incidents, or engineer-hours spent maintaining per-agent settings]
**Emotional tension:** Fear of an unreviewed agent running shell commands or reading secrets in a sensitive repo, and of not being able to demonstrate control to auditors. (Inferred, not from customers.)

## Competitive Landscape
**Direct:** No like-for-like multi-agent, per-repo policy product identified in the repo. [NEED: competitor scan; none is named by the founder]
**Secondary (different solution, same problem):**
- Claude Code managed settings, MDM and server-managed settings: org-uniform, Claude only.
- Anthropic self-hosted Claude apps gateway: SSO and per-IdP-group settings, requires own API key or cloud provider.
- OpenAI Agent Security for Codex: per-group on ChatGPT plans, Codex only.
**Indirect:** Hand-maintained settings files and internal scripts; written acceptable-use policy and trust; blocking agents entirely.

## Differentiation
**Key differentiators:**
- One policy across Claude Code, Codex and Gemini CLI, with per-group and per-repository targeting.
- Enforcement at launch through `policyHelper`: bare `claude` is still governed; no wrapper needed for Claude.
- Signed bundles verified before write, Ed25519 seed or AWS KMS key, with rotation that needs no re-enrollment.
- Fails safe: offline helper, always exits 0 by default; `requireBundle` opt-in for strict orgs.
- Open source (Apache-2.0), self-host or hosted cell; builds on vendor managed settings instead of replacing them.
**How we do it differently:** A control plane plus root-owned sync plus a launch-time helper, writing into each agent's own managed tier rather than inventing a new enforcement layer.
**Why that's better:** Vendor-supported mechanisms mean less to break; one source of truth; auditable by reading the code.
**Why customers choose us:** [NEED: win/loss or design-partner reasons]
**Honest limits (use in copy; the product's credibility rests on them):** Codex and Gemini repo-scoped rules are advisory (apply via `aw codex` / `aw gemini`); signing is detection, not prevention, against root; repo scoping keys on `.git/config` origin, which a developer can edit; server-managed settings from the claude.ai console shadow the helper; the hosted console is not yet available.

## Objections
| Objection | Response |
|-----------|----------|
| "Can developers bypass it?" | Not for Claude Code without local admin rights; the helper runs on every launch. Codex and Gemini machine-wide files always apply; repo rules need `aw codex`/`aw gemini`. |
| "What if the control plane is down?" | Nothing changes. The helper reads the last synced bundle from disk. |
| "Does it replace Claude Code managed settings?" | No, it builds on them via a drop-in that names the helper. |
| "Another thing to run and secure." | Hosted cells (pilot) or self-host; trust statement and runbook for hosted. [NEED: SOC 2 / security attestations, if any] |
| "Why not wait for vendors to ship this?" | Vendors are adding per-group controls per agent, so the lasting value is cross-agent and per-repo. |

**Anti-persona:** Solo developers and small teams with one agent and no repo-level distinctions; orgs on a single vendor whose console already covers their needs; buyers wanting a SaaS they can self-serve today (hosted console is not live).

## Switching Dynamics
**Push:** Org-uniform vendor settings; a new agent adopted by a team with no controls; audit asks.
**Pull:** One policy, per-repo and per-group rules, signed delivery, no developer workflow change.
**Habit:** Settings files in MDM or a repo; "we trust devs" norms.
**Anxiety:** Breaking developer launches (answer: always exits 0), another service to run, early-stage project ("Status: early" in README), trusting a new vendor's signing keys.

## Customer Language
No customer interviews or quotes exist in the repo.
**How they describe the problem:** [NEED: verbatim from security/platform leads]
**How they describe us:** [NEED: verbatim from design partners]
**Words to use (from site copy):** one policy, every coding agent, govern, group and repository targeting, signed bundles, fails safe, enforce at launch, control plane.
**Words to avoid:** "unbreakable", "tamper-proof", "bypass-proof" (the docs state limits); "AI safety" framing; implying hosted console is live.
**Glossary:**
| Term | Meaning |
|------|---------|
| awd | The control plane server and CLI (`awd apply` authors policy) |
| aw-sync | Root-side sync agent; fetches, verifies, writes each agent's files |
| aw-policy / policyHelper | Offline helper Claude Code runs at launch to compute managed settings |
| aw | Wrapper CLI: `aw claude`, `aw codex`, `aw gemini`, `aw doctor` |
| Bundle | Per-machine, optionally signed, compiled policy |
| Cell | One isolated hosted control plane per tenant |

## Brand Voice
Derived from site copy only; no founder writing sample beyond the README.
| Attribute | Means | Doesn't mean | Example line |
|-----------|-------|--------------|--------------|
| Precise | Name the mechanism, flag, file | Vague "AI-powered" claims | "Claude Code runs the helper on every start, including a bare claude" |
| Candid | State limits next to claims | Hiding caveats | "This layer is advisory: bare codex or gemini sees the machine-wide files alone." |
| Operator-calm | Fail-safe, quiet, no fear marketing | Alarmism | "Nothing changes for developers." |
| Plain | Short declaratives | Jargon stacks, hype | "Write your organization's rules once." |

**Tone by context:**
- Support: exact commands and expected output.
- Launch: what ships and what does not yet (mark "Coming soon").
- Errors: say what refused and why.
- Sales: lead with the buyer's own current rules ("Bring the rules you enforce today").
- Social: [NEED: channel and founder voice samples]
**Terms we own:** control plane, policy bundle, launch-time enforcement, "one policy for every coding agent".
**Never use (and why):** unsupported superlatives and "secure" without scope, because the docs deliberately separate detection from prevention.

## Proof Points
**Metrics:** None in repo. [NEED: fleet size, machines governed, enforcement or latency numbers]
**Customers:** None named. Hosted-cells spec refers to "design partners" without names. [NEED: permission-cleared logos]
**Testimonials:** [NEED: quote from a design partner or pilot user]
**Value themes:**
| Theme | Proof |
|-------|-------|
| Cross-agent, per-repo targeting | Claude, Codex, Gemini adapters; repo globs and group matching (README, product page) |
| Tamper evidence | Ed25519 and AWS KMS signing, verified before write, `aw doctor` reports state |
| Safe by default | Helper exits 0 offline; schema-validated output |
| Standards-based provisioning | SCIM 2.0 for Okta, Entra ID |
| Inspectable | Apache-2.0 source; hosted cells have a one-page trust statement and runbook |
[NEED: third-party security review or pen test, if one exists]

## Goals
**Business goal:** Land design partners on hosted cells, then open the hosted console and paid tiers. [NEED: founder-confirmed target, e.g. number of partners]
**Conversion action:** Request demo (`/demo`, optionally `?plan=team|enterprise|self-hosted`).
**Current metrics:** [NEED: demo requests, pilot count, GitHub stars/installs]

## Paid Ads
Skipped by instruction.

## Learnings
*Decisions and tested lessons. Newest first. Keep it to about 20 active entries.*
| Date | Learning | Evidence | Applies to | Status |
|------|----------|----------|------------|--------|
| | | | | |

## Changelog
*Newest first. One line per revision: what changed and why.*
- v1 (2026-10-24) — Initial context, auto-drafted from the repo for review.
