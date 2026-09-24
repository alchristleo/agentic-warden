# Marketing site: commercialising Agentic Warden

Date: 2026-09-22, finalised 2026-09-24. Priority: lowest — after signed
bundles, SCIM, install-timer and the deferred minors (all shipped).
Status: **design approved; ready for an implementation plan.**

## Scope

A static marketing site: landing, product, pricing, a demo-request form, a
link out to the docs. No login, no database, no billing. The hosted console
is presented as a coming-soon feature (see "Honesty rules") but is not
built here; it is roadmap item (e) and opens with a trust-model brainstorm
(signing-key custody, multi-tenancy), not UI.

Brand: "Agentic Warden" — enterprise control plane for AI coding agents;
govern Claude Code, Codex and Gemini CLI from one policy. Visual direction:
dark developer-tool default with light mode, monospace accents, terminal and
policy-YAML snippets as hero content.

## Stack

Next.js 16 App Router (owner ruling 2026-09-24: 15 is backport-only),
TypeScript strict, Tailwind v4 (`@theme`), shadcn/ui (new-york,
`next-themes`), `lucide-react`, Geist Sans + Geist Mono, zod. The hero
animates with CSS keyframes rather than Framer Motion, whose server render
leaves the terminal at `opacity: 0` without JavaScript. Node 20, pnpm. Tests: Vitest, Playwright,
`@axe-core/playwright`, Lighthouse CI (`@lhci/cli`).

Location: `web/` in this repo as its own pnpm project. Root `.gitignore`
gains `web/node_modules`, `web/.next` and `web/test-results`. The Go module
and Go CI are untouched.

## Agent skills, installed project-scoped into `.claude/skills/`

| Skill | Install |
| --- | --- |
| next-best-practices | `npx skills add vercel-labs/next-skills -a claude-code` |
| vercel-react-best-practices | `npx skills add vercel-labs/agent-skills --skill vercel-react-best-practices -a claude-code` |
| shadcn (official) | `npx shadcn@latest add skills` |
| tailwind-design-system | `npx skills add wshobson/agents --skill tailwind-design-system -a claude-code` |
| frontend-design (impeccable) | `npx skills add pbakaus/impeccable --skill frontend-design -a claude-code` |
| ui-animation | `npx skills add mblode/agent-skills --skill ui-animation -a claude-code` |
| web-quality | `npx skills add addyosmani/web-quality-skills -a claude-code` |
| playwright-best-practices | `npx skills add currents-dev/playwright-best-practices-skill -a claude-code` |
| seo-mastery (MIT) | `npx skills add kpab/seo-mastery-agent-skills --skill seo-mastery -a claude-code` |

The upstream README for seo-mastery documents only a plugin install
(`claude plugin install seo-mastery@seo-mastery-agent-skills`); if the
`npx skills` form fails, copy `skills/seo-mastery/` into `.claude/skills/`
by hand rather than installing a user-wide plugin. Any further skill is
proposed to the owner before install.

## Pages

All routes are statically rendered except `/demo`, which renders per request so
`?plan=` pre-fill and the spam timer work without JavaScript.

| Route | Content |
| --- | --- |
| `/` | Hero ("One policy for every coding agent", animated `aw claude` terminal + policy YAML). Problem: what the vendor consoles and gateways leave uncovered — per-repository targeting, per-group policy on seat plans, agents other than Claude Code. How it works in three steps: `awd` → `aw-sync` → `policyHelper`. Supported agents strip. CTA: Request demo. |
| `/product` | One section per shipped capability: group and repository targeting; signed bundles; SCIM provisioning (Okta, Entra ID); `aw-sync` timer on systemd, launchd and Task Scheduler; a helper that fails safe so Claude Code always starts; `aw doctor`. Final section: hosted console, badged "Coming soon". |
| `/pricing` | Three tier cards — Team ("Hosted — coming soon"), Enterprise, Self-hosted — every one priced "Contact us". Feature comparison table. FAQ. |
| `/demo` | Demo-request form. Every CTA links here with `?plan=team|enterprise|self-hosted`, which pre-selects the plan. |
| Docs | Header link to the GitHub README. No docs pages in this milestone. |
| `not-found` | Branded 404. |

Shared layout: header (logo, Product, Pricing, Docs ↗, theme toggle,
"Request demo" button) and footer (GitHub, licence, © line). No login link.

### Content model

Copy lives in typed TypeScript modules — `web/content/{site,home,product,pricing}.ts`
— and components render from them. No MDX, no CMS: there is no marketing
team yet, and typed data lets the build catch a missing field.

### Honesty rules

- Every product claim maps to shipped code. The hosted console is the only
  unshipped feature named, and always carries the "Coming soon" badge.
- Console copy describes outcomes only (manage policy, groups and enrolled
  machines from a browser). It says nothing about where signing keys live
  or how customer data is stored — that design is undecided.
- No customer logos, testimonials, user counts or benchmark numbers.
- No prices: there is no licence or billing mechanism to enforce one.

## Demo-request form

Fields (zod schema shared by client and server):

| Field | Rule |
| --- | --- |
| name | required, 1–100 chars |
| email | required, valid email, ≤ 254 chars; a webmail domain (gmail.com, outlook.com, yahoo.com, icloud.com, proton.me) shows a non-blocking hint |
| company | required, 1–100 chars |
| size | required: `1-50`, `51-500`, `500+` |
| agents | multi-select, ≥ 1 of: `claude-code`, `codex`, `gemini-cli`, `other` |
| deployment | required: `self-hosted`, `hosted`, `unsure` |
| plan | optional: `team`, `enterprise`, `self-hosted`; pre-filled from `?plan=`, unknown values ignored |
| message | optional, ≤ 2000 chars |

Submission is a Next.js server action driven by `useActionState`, so the form
works with JavaScript disabled. The action returns
`{ status: "ok" } | { status: "invalid", fieldErrors } | { status: "failed" }`.

Spam: a hidden honeypot field and a hidden render timestamp (unsigned — a
speed bump for naive bots, not a defence); a submission with the honeypot
filled or completed in under 3 seconds returns
`{ status: "ok" }` and sends nothing. No captcha; add Cloudflare Turnstile
only if spam appears.

Delivery: Resend sends one plain-text email to `DEMO_INBOX` from `DEMO_FROM`,
subject `Demo request: <company> (<plan or "no plan">)`, `reply-to` the
submitter. No confirmation email goes to the submitter — that would let
anyone make the site mail an arbitrary address. The sender is injected, so
tests replace it without network access.

### Failure handling

| Condition | Behaviour |
| --- | --- |
| Validation fails | `invalid`; errors render inline beside each field, focus moves to the first; entered values stay. |
| Honeypot or < 3 s | `ok`, nothing sent. |
| Resend returns an error or throws | `failed`; form keeps its values and shows "Couldn't send — email us at `<DEMO_INBOX>`" with a `mailto:` link. Server logs the error class and status, never field values. |
| `RESEND_API_KEY`, `DEMO_INBOX` or `DEMO_FROM` unset | Production build fails. In development the action returns `failed`. |
| `NEXT_PUBLIC_SITE_URL` unset | Production build fails. |

## SEO

- Per-route `metadata`: unique title and description, canonical URL built
  from `NEXT_PUBLIC_SITE_URL`; no domain is hard-coded.
- `app/sitemap.ts` lists every public route; `app/robots.ts` allows all in
  production and disallows all when `VERCEL_ENV !== "production"`, and
  preview builds also emit `noindex` metadata.
- `opengraph-image` per route (generated, brand typography).
- JSON-LD: `Organization` and `SoftwareApplication` site-wide, `FAQPage` on
  `/pricing` built from the same data as the visible FAQ.
- `public/llms.txt` summarising the product and linking each page and the
  README.

## Security headers

Set in `next.config`: Content-Security-Policy with `default-src 'self'`,
`script-src 'self' 'unsafe-inline'` (no `unsafe-eval`), `object-src 'none'`,
`base-uri 'self'`, `form-action 'self'`. Nonces are rejected: in Next.js they
force every route to render per request, which breaks static rendering, and
the site has no user content to inject, so inline-script risk is low. Also
`Strict-Transport-Security`, `frame-ancestors 'none'`,
`Referrer-Policy: strict-origin-when-cross-origin`, and a
`Permissions-Policy` denying camera, microphone and geolocation.

## Testing

- **Vitest**: the schema (every row of the field table, webmail hint), the
  action (ok, invalid, honeypot, too-fast, sender failure, missing env) with
  the sender mocked, and that failure logs contain no field values.
- **Playwright**: every route renders; header navigation; theme toggle
  persists; form happy path, inline field errors, sender-failure fallback
  with the `mailto:` link, submission with JavaScript disabled, `?plan=`
  pre-fill. The sender is replaced through a test-only env switch that a
  production build rejects.
- **Accessibility**: `@axe-core/playwright` on every route in both themes,
  zero violations.
- **SEO**: every route has a unique title, description and canonical; the
  sitemap lists every route; each JSON-LD block parses; a preview build
  serves `noindex` and a disallow-all robots.txt.
- **Lighthouse CI** (mobile, every route) as a gate: Performance ≥ 90,
  Accessibility = 100, Best Practices ≥ 95, SEO ≥ 95.

## CI and deploy

`.github/workflows/web.yml`, triggered only by changes under `web/**` (and
the workflow itself): pnpm install, lint, typecheck, Vitest, build,
Playwright, Lighthouse CI. The Go workflow is untouched.

Vercel project with root directory `web/`. Environment: `RESEND_API_KEY`,
`DEMO_INBOX`, `DEMO_FROM`, `NEXT_PUBLIC_SITE_URL`. Launch needs a domain and
a Resend account with a verified sender domain; neither blocks the build,
since every test mocks the sender. The console, when built, lives on its own
`app.` subdomain as a separate deployment.

## Rejected

Headless CMS (no marketing team yet); Astro (owner prefers Next.js); Stripe
checkout (no licence mechanism in awd); route handler for the form (server
action gives no-JS submission for free); captcha up front (friction before
any spam exists); confirmation email to the submitter (mail relay);
ui-ux-pro-max (direction already chosen); react-doctor (web-quality covers
it); AgriciDaniel/claude-seo (audit suite with paid-API extensions, oversized
for four pages — revisit post-launch); seranking/seo-skills (needs the paid
SE Ranking MCP server); building the hosted console in this milestone.
