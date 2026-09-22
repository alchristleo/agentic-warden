# Marketing site: commercialising Agentic Warden

Date: 2026-09-22. Status: **brainstorm paused after section 1 (stack +
skills approved)**. Sections 2 (pages, content, data flow) and 3 (demo
form, errors, testing, deploy) still to present and approve; then the spec
is finalised and a plan written.

## Decisions so far

- Scope: marketing site only. Landing, product/features, pricing, docs
  link, "Request demo" form. No login. SaaS console is a later milestone.
- Location and deploy: `web/` in this repo as its own pnpm project;
  Vercel deploys from the subdirectory. Go CI untouched.
- Commerce: pricing page shows tiers (Team / Enterprise / Self-hosted);
  CTAs open a demo-request form posting to a Next.js route handler that
  emails via Resend. No database. No Stripe in this milestone.
- Brand: "Agentic Warden" — enterprise control plane for AI coding agents;
  govern Claude Code, Codex and Gemini CLI from one policy.
- Visual direction: dark developer-tool default with light mode, monospace
  accents, terminal and policy-YAML snippets as hero content.

## Stack (approved)

Next.js 15 App Router, TypeScript strict, Tailwind v4 (`@theme`),
shadcn/ui (new-york, `next-themes`), `lucide-react`, Framer Motion for the
hero only, Geist Sans + Geist Mono. Node 20, pnpm. Root `.gitignore` gains
`web/node_modules` and `web/.next`.

## Agent skills (approved), installed project-scoped into `.claude/skills/`

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

Source list: https://github.com/finfin/awesome-frontend-skills. Any further
skill is proposed to the owner before install.

## Rejected

Headless CMS (no marketing team yet); Astro (owner prefers Next.js);
Stripe checkout (no licence mechanism in awd); ui-ux-pro-max (direction
already chosen); react-doctor (web-quality covers it).
