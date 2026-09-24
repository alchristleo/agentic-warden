# Agentic Warden marketing site

This is the marketing site for Agentic Warden — home, product, pricing and
demo-request pages. It's a standalone Next.js project with its own pnpm
lockfile and does not share code with the Go module at the repository root.

## Commands

Run everything from this directory with pnpm; other package managers aren't
supported here.

    pnpm install
    pnpm dev            # dev server on http://localhost:3000
    pnpm lint           # eslint .
    pnpm typecheck      # tsc --noEmit
    pnpm test           # unit tests (vitest)
    pnpm test:e2e       # Playwright, against a production build (next build && next start)
    pnpm lhci           # Lighthouse CI, against a production build

## Environment

Production deploys (`VERCEL_ENV=production`) require `NEXT_PUBLIC_SITE_URL`,
`RESEND_API_KEY`, `DEMO_INBOX` and `DEMO_FROM`; the build fails if any is
missing. See `.env.example` for the full list, and the root README's
"Marketing site" section for how this project fits into the repository.
