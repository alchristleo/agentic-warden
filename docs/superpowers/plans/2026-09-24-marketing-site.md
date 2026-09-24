# Marketing Site Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Agentic Warden marketing site in `web/` — landing, product, pricing, demo-request form — with SEO, security headers, accessibility and Lighthouse gates in CI.

**Architecture:** A standalone Next.js 16 App Router project in `web/`, deployed to Vercel from that subdirectory. Copy lives in typed modules under `web/content/`; pages render from them. The only server-side behaviour is one server action that validates a demo request with zod and emails it through an injected sender (Resend in production, a fake in tests).

**Tech Stack:** Next.js 16, React 19, TypeScript strict, Tailwind v4, shadcn/ui (new-york), next-themes, lucide-react, geist, zod, resend; Vitest, Playwright, @axe-core/playwright, @lhci/cli. Node 20, pnpm 10.

**Spec:** `docs/superpowers/specs/2026-09-22-marketing-site-design.md`

## Global Constraints

- Everything lives under `web/`. Do not touch any Go code, `go.mod`, or anything outside `web/`, `.claude/skills/`, `.github/workflows/web.yml`, the root `.gitignore` and the root `README.md`.
- Next.js **16** (owner ruling 2026-09-24, supersedes the spec's "15"). Node 20, pnpm. TypeScript `strict: true`.
- Next 16 has no `next lint`; lint is `eslint .`. Route `params`/`searchParams` are Promises and must be awaited.
- Styling: Tailwind v4 via `@theme` in `app/globals.css`; shadcn/ui new-york. Fonts: Geist Sans + Geist Mono from the `geist` package. Icons: `lucide-react`. No animation library (see rulings).
- Dark theme is the default (`next-themes`, `attribute="class"`, `defaultTheme="dark"`, `enableSystem={false}`); light mode via the header toggle.
- Form controls in the demo form are native `<input>`, `<select>`, `<textarea>` (styled), not Radix checkbox/radio/select — the form must submit with JavaScript disabled. FAQ uses native `<details>`/`<summary>` so answers are in the HTML.
- **Honesty rules (verbatim from spec):** Every product claim maps to shipped code. The hosted console is the only unshipped feature named, and always carries the "Coming soon" badge. Console copy describes outcomes only (manage policy, groups and enrolled machines from a browser). It says nothing about where signing keys live or how customer data is stored. No customer logos, testimonials, user counts or benchmark numbers. No prices: every tier is priced "Contact us".
- Terminal snippets show commands only, never invented command output.
- No domain is hard-coded. Absolute URLs come from `siteUrl()` (`NEXT_PUBLIC_SITE_URL`, default `http://localhost:3000`).
- "Production" means `VERCEL_ENV === "production"`. In production the build fails if any of `NEXT_PUBLIC_SITE_URL`, `RESEND_API_KEY`, `DEMO_INBOX`, `DEMO_FROM` is unset, or if the test-only `DEMO_SENDER` is set.
- No confirmation email to the submitter. Server logs for a failed send contain the error name and status only, never field values.
- Honeypot filled, or submission under 3 seconds after render → return `{ status: "ok" }` and send nothing. A missing timestamp skips the time check (the honeypot still applies).
- Preview builds (`VERCEL_ENV !== "production"`) disallow all crawlers in `robots.txt` and emit `noindex` metadata.
- No analytics, no database, no login link, no captcha.
- Accessibility: zero axe violations on every route in both themes. Lighthouse (mobile) gates: Performance ≥ 0.9, Accessibility = 1, Best Practices ≥ 0.95, SEO ≥ 0.95.
- Commit messages end with:
  `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01UENwpnQH7LP5N1SFDRbxUg`
- Before writing code in an area, read the matching project skill in `.claude/skills/` (installed in Task 1): next-best-practices, vercel-react-best-practices, shadcn, tailwind-design-system, frontend-design, ui-animation, web-quality, playwright-best-practices, seo-mastery. Install no other skill.

## Rulings made while planning

- `/demo` renders per request (it reads `searchParams` for `?plan=` and stamps the render time server-side, so both work without JavaScript). Every other route is static. Cost if wrong: one dynamic route; Lighthouse still measures it.
- The Playwright suite runs against `next build && next start` with `DEMO_SENDER=fake`. The fake sender fails when the company field is exactly `__fail__`, which is how the failure fallback is tested end to end.
- Preview-mode behaviour (robots, noindex) is tested in Vitest through pure functions taking an env object, not with a second build.
- The hero animates with CSS keyframes, not Framer Motion (supersedes the spec's allowance). Framer Motion's server render emits `opacity: 0` for the animation's first frame, so without JavaScript the terminal would stay invisible, and it adds client JS that costs Lighthouse Performance. A CSS animation with `animation-fill-mode: both` under `motion-safe:` plays without JavaScript and is skipped under reduced motion. Cost if wrong: re-add framer-motion for richer motion later.
- OG images use the Geist font if a `.ttf`/`.otf` file ships in `node_modules/geist`; Satori cannot read `.woff2`, so if only `.woff2` exists, fall back to the default font and note it in the report.

## File Structure

```
web/
  package.json, pnpm-lock.yaml, tsconfig.json, next.config.ts, eslint.config.mjs,
  postcss.config.mjs, components.json, vitest.config.ts, playwright.config.ts,
  lighthouserc.json, .gitignore, .env.example
  app/
    layout.tsx            root layout, fonts, theme, header/footer, site JSON-LD
    globals.css           Tailwind v4 + shadcn tokens
    page.tsx              home
    product/page.tsx
    pricing/page.tsx
    demo/page.tsx         dynamic; reads ?plan=
    demo/actions.ts       "use server" wrapper around submitDemo
    not-found.tsx
    sitemap.ts, robots.ts
    llms.txt/route.ts
    opengraph-image.tsx, product/opengraph-image.tsx, pricing/opengraph-image.tsx, demo/opengraph-image.tsx
  components/
    ui/*                  shadcn: button, card, badge, input, textarea, label, table
    theme-provider.tsx, theme-toggle.tsx, site-header.tsx, site-footer.tsx
    json-ld.tsx
    home/hero.tsx, home/terminal.tsx
    demo/demo-form.tsx
  content/
    site.ts, home.ts, product.ts, pricing.ts
  lib/
    env.ts                env reading + build assertion
    seo.ts                pageMetadata(), robotsFor(), jsonLd builders
    og.tsx                shared OG image renderer
    demo/schema.ts        zod schema, option lists, isWebmail()
    demo/email.ts         formatEmail()
    demo/sender.ts        Sender type, resendSender(), fakeSender(), senderFromEnv()
    demo/submit.ts        submitDemo() — pure, deps injected
    utils.ts              cn() (from shadcn)
  e2e/*.spec.ts           Playwright
  lib/**/*.test.ts        Vitest
.github/workflows/web.yml
```

---

### Task 1: Scaffold `web/`, tooling, env module, project skills

**Files:**
- Create: `web/` (via create-next-app), `web/lib/env.ts`, `web/lib/env.test.ts`, `web/vitest.config.ts`, `web/playwright.config.ts`, `web/e2e/smoke.spec.ts`, `web/.env.example`
- Create: `.claude/skills/*` (skill installs)
- Modify: root `.gitignore`, `web/package.json`, `web/next.config.ts`

**Interfaces:**
- Produces: `siteUrl(env?): string`, `isProductionDeploy(env?): boolean`, `assertBuildEnv(env?): void`, type `Env`; `cn()` in `@/lib/utils`; path alias `@/*` → `web/*`; scripts `lint`, `typecheck`, `test`, `test:e2e`, `build`, `start`.

- [ ] **Step 1: Install the project skills** (repo root). Run each; if one fails, record the exact error in the report and continue — do not substitute another skill.

```bash
npx -y skills add vercel-labs/next-skills -a claude-code
npx -y skills add vercel-labs/agent-skills --skill vercel-react-best-practices -a claude-code
npx -y skills add wshobson/agents --skill tailwind-design-system -a claude-code
npx -y skills add pbakaus/impeccable --skill frontend-design -a claude-code
npx -y skills add mblode/agent-skills --skill ui-animation -a claude-code
npx -y skills add addyosmani/web-quality-skills -a claude-code
npx -y skills add currents-dev/playwright-best-practices-skill -a claude-code
npx -y skills add kpab/seo-mastery-agent-skills --skill seo-mastery -a claude-code
```

If the seo-mastery install fails, clone `https://github.com/kpab/seo-mastery-agent-skills` to a temp dir and copy its `skills/seo-mastery/` directory to `.claude/skills/seo-mastery/`. The shadcn skill is installed in Step 3 after `web/` exists. Confirm `ls .claude/skills` lists each skill directory. If an installer asks for project vs global scope, choose project.

- [ ] **Step 2: Scaffold the app**

```bash
pnpm create next-app@16 web --ts --tailwind --eslint --app --no-src-dir --import-alias "@/*" --use-pnpm --yes
```

Verify `web/package.json` has `next` 16.x and `react` 19.x, `web/tsconfig.json` has `"strict": true`, and `web/app/globals.css` starts with `@import "tailwindcss";`. Delete the starter's demo SVGs from `web/public/` and replace `web/app/page.tsx` with:

```tsx
export default function Home() {
  return <h1>Agentic Warden</h1>;
}
```

- [ ] **Step 3: shadcn, its skill, and dependencies**

```bash
cd web
pnpm dlx shadcn@latest init -d
pnpm dlx shadcn@latest add button card badge input textarea label table
pnpm dlx shadcn@latest add skills   # installs the shadcn agent skill; if this subcommand does not exist, record it and continue
pnpm add next-themes lucide-react geist zod resend
pnpm add -D vitest @playwright/test @axe-core/playwright @lhci/cli
pnpm exec playwright install chromium
```

Confirm `components.json` has `"style": "new-york"`. If `shadcn add skills` wrote into `web/.claude/`, move that skill directory to the repo-root `.claude/skills/`.

- [ ] **Step 4: Scripts.** In `web/package.json` set:

```json
"scripts": {
  "dev": "next dev",
  "build": "next build",
  "start": "next start",
  "lint": "eslint .",
  "typecheck": "tsc --noEmit",
  "test": "vitest run",
  "test:e2e": "playwright test",
  "lhci": "lhci autorun"
}
```

- [ ] **Step 5: Write the failing env test** — `web/lib/env.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { assertBuildEnv, isProductionDeploy, siteUrl } from "./env";

const prod = {
  VERCEL_ENV: "production",
  NEXT_PUBLIC_SITE_URL: "https://example.test",
  RESEND_API_KEY: "re_x",
  DEMO_INBOX: "sales@example.test",
  DEMO_FROM: "site@example.test",
};

describe("siteUrl", () => {
  it("defaults to localhost", () => {
    expect(siteUrl({})).toBe("http://localhost:3000");
  });
  it("strips trailing slashes", () => {
    expect(siteUrl({ NEXT_PUBLIC_SITE_URL: "https://example.test//" })).toBe("https://example.test");
  });
});

describe("isProductionDeploy", () => {
  it("is true only for VERCEL_ENV=production", () => {
    expect(isProductionDeploy({ VERCEL_ENV: "production" })).toBe(true);
    expect(isProductionDeploy({ VERCEL_ENV: "preview" })).toBe(false);
    expect(isProductionDeploy({})).toBe(false);
  });
});

describe("assertBuildEnv", () => {
  it("accepts a complete production env", () => {
    expect(() => assertBuildEnv(prod)).not.toThrow();
  });
  it("ignores missing vars outside production", () => {
    expect(() => assertBuildEnv({ VERCEL_ENV: "preview" })).not.toThrow();
    expect(() => assertBuildEnv({})).not.toThrow();
  });
  it.each(["NEXT_PUBLIC_SITE_URL", "RESEND_API_KEY", "DEMO_INBOX", "DEMO_FROM"])(
    "fails production when %s is unset",
    (key) => {
      const env: Record<string, string | undefined> = { ...prod, [key]: undefined };
      expect(() => assertBuildEnv(env)).toThrow(key);
    },
  );
  it("fails production when the test-only sender is set", () => {
    expect(() => assertBuildEnv({ ...prod, DEMO_SENDER: "fake" })).toThrow("DEMO_SENDER");
  });
});
```

`web/vitest.config.ts`:

```ts
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: { alias: { "@": fileURLToPath(new URL(".", import.meta.url)) } },
  test: {
    environment: "node",
    include: ["lib/**/*.test.ts", "app/**/*.test.ts"],
  },
});
```

- [ ] **Step 6: Run it to verify it fails**

Run: `cd web && pnpm test`
Expected: FAIL — cannot resolve `./env`.

- [ ] **Step 7: Implement** `web/lib/env.ts`:

```ts
export type Env = Record<string, string | undefined>;

const REQUIRED_IN_PRODUCTION = [
  "NEXT_PUBLIC_SITE_URL",
  "RESEND_API_KEY",
  "DEMO_INBOX",
  "DEMO_FROM",
] as const;

export function isProductionDeploy(env: Env = process.env): boolean {
  return env.VERCEL_ENV === "production";
}

export function siteUrl(env: Env = process.env): string {
  return (env.NEXT_PUBLIC_SITE_URL || "http://localhost:3000").replace(/\/+$/, "");
}

// Called from next.config.ts so a misconfigured production deploy fails at
// build time rather than on the first demo request.
export function assertBuildEnv(env: Env = process.env): void {
  if (!isProductionDeploy(env)) return;
  const missing = REQUIRED_IN_PRODUCTION.filter((key) => !env[key]);
  if (missing.length > 0) {
    throw new Error(`missing required environment: ${missing.join(", ")}`);
  }
  if (env.DEMO_SENDER) {
    throw new Error("DEMO_SENDER is test-only and must not be set in production");
  }
}
```

In `web/next.config.ts`, add at the top (below the `NextConfig` type import) and keep the exported config otherwise as generated:

```ts
import { assertBuildEnv } from "./lib/env";

assertBuildEnv();
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd web && pnpm test`
Expected: PASS (all env tests).

- [ ] **Step 9: Playwright harness + smoke test.** `web/playwright.config.ts`:

```ts
import { defineConfig, devices } from "@playwright/test";

const port = 3000;
const baseURL = `http://localhost:${port}`;

export default defineConfig({
  testDir: "e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? "github" : "list",
  use: { baseURL, trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "pnpm build && pnpm start",
    url: baseURL,
    reuseExistingServer: !process.env.CI,
    timeout: 240_000,
    env: {
      DEMO_SENDER: "fake",
      DEMO_INBOX: "sales@example.test",
      NEXT_PUBLIC_SITE_URL: baseURL,
    },
  },
});
```

`web/e2e/smoke.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

test("home page renders", async ({ page }) => {
  const response = await page.goto("/");
  expect(response?.status()).toBe(200);
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
});
```

`web/.env.example`:

```
# Required when VERCEL_ENV=production; the build fails without them.
NEXT_PUBLIC_SITE_URL=https://example.com
RESEND_API_KEY=
DEMO_INBOX=
DEMO_FROM=
# Test-only: "fake" swaps Resend for an in-process fake. Never set in production.
# DEMO_SENDER=fake
```

- [ ] **Step 10: Ignore build output.** Append to the root `.gitignore`:

```
# Marketing site (web/)
web/node_modules
web/.next
web/test-results
web/playwright-report
web/.lighthouseci
web/.env*.local
```

- [ ] **Step 11: Verify everything**

Run: `cd web && pnpm lint && pnpm typecheck && pnpm test && pnpm test:e2e`
Expected: all pass; the smoke test passes against the production build.

- [ ] **Step 12: Commit**

```bash
git add .gitignore .claude/skills web
git commit -m "feat(web): scaffold Next.js 16 marketing site with env guard and test harness"
```

---

### Task 2: Site shell — fonts, theme, header, footer, 404

**Files:**
- Create: `web/content/site.ts`, `web/components/theme-provider.tsx`, `web/components/theme-toggle.tsx`, `web/components/site-header.tsx`, `web/components/site-footer.tsx`, `web/app/not-found.tsx`, `web/e2e/shell.spec.ts`
- Modify: `web/app/layout.tsx`, `web/app/globals.css`

**Interfaces:**
- Consumes: `cn` from `@/lib/utils`; shadcn `Button`.
- Produces: `site` (exported const, shape below) from `@/content/site`; `<ThemeToggle />`; layout wraps every page in header + `<main id="main">` + footer.

- [ ] **Step 1: Write the failing shell test** — `web/e2e/shell.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

test("header links point at every section", async ({ page }) => {
  await page.goto("/");
  const header = page.getByRole("banner");
  await expect(header.getByRole("link", { name: "Product" })).toHaveAttribute("href", "/product");
  await expect(header.getByRole("link", { name: "Pricing" })).toHaveAttribute("href", "/pricing");
  await expect(header.getByRole("link", { name: /Docs/ })).toHaveAttribute(
    "href",
    "https://github.com/alchristleo/agentic-warden#readme",
  );
  await expect(header.getByRole("link", { name: "Request demo" })).toHaveAttribute("href", "/demo");
  await expect(header.getByRole("link", { name: /log ?in|sign ?in/i })).toHaveCount(0);
});

test("dark is the default theme and the toggle persists", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("html")).toHaveClass(/dark/);
  await page.getByRole("button", { name: "Toggle theme" }).click();
  await expect(page.locator("html")).not.toHaveClass(/dark/);
  await page.reload();
  await expect(page.locator("html")).not.toHaveClass(/dark/);
});

test("unknown routes get the branded 404", async ({ page }) => {
  const response = await page.goto("/no-such-page");
  expect(response?.status()).toBe(404);
  await expect(page.getByRole("heading", { level: 1, name: "Page not found" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Back to home" })).toHaveAttribute("href", "/");
});

test("footer carries GitHub and licence links", async ({ page }) => {
  await page.goto("/");
  const footer = page.getByRole("contentinfo");
  await expect(footer.getByRole("link", { name: "GitHub" })).toHaveAttribute(
    "href",
    "https://github.com/alchristleo/agentic-warden",
  );
  await expect(footer.getByRole("link", { name: "Apache-2.0" })).toBeVisible();
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && pnpm test:e2e e2e/shell.spec.ts`
Expected: FAIL — no banner/links.

- [ ] **Step 3: Content** — `web/content/site.ts`:

```ts
export const site = {
  name: "Agentic Warden",
  tagline: "One policy for every coding agent",
  description:
    "Agentic Warden is the enterprise control plane for AI coding agents. Author one policy and govern Claude Code, Codex and Gemini CLI by group and by repository.",
  githubUrl: "https://github.com/alchristleo/agentic-warden",
  docsUrl: "https://github.com/alchristleo/agentic-warden#readme",
  licenseName: "Apache-2.0",
  licenseUrl: "https://github.com/alchristleo/agentic-warden/blob/main/LICENSE",
  nav: [
    { href: "/product", label: "Product" },
    { href: "/pricing", label: "Pricing" },
  ],
} as const;
```

- [ ] **Step 4: Theme components.** `web/components/theme-provider.tsx`:

```tsx
"use client";

import { ThemeProvider as NextThemesProvider } from "next-themes";
import type { ComponentProps } from "react";

export function ThemeProvider(props: ComponentProps<typeof NextThemesProvider>) {
  return <NextThemesProvider {...props} />;
}
```

`web/components/theme-toggle.tsx`:

```tsx
"use client";

import { Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { Button } from "@/components/ui/button";

export function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme();
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label="Toggle theme"
      onClick={() => setTheme(resolvedTheme === "dark" ? "light" : "dark")}
    >
      <Sun className="hidden size-4 dark:block" aria-hidden />
      <Moon className="size-4 dark:hidden" aria-hidden />
    </Button>
  );
}
```

- [ ] **Step 5: Header and footer.** `web/components/site-header.tsx`:

```tsx
import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import { site } from "@/content/site";
import { Button } from "@/components/ui/button";
import { ThemeToggle } from "@/components/theme-toggle";

export function SiteHeader() {
  return (
    <header className="sticky top-0 z-40 border-b border-border/60 bg-background/80 backdrop-blur">
      <div className="mx-auto flex h-14 max-w-6xl items-center gap-6 px-4 sm:px-6">
        <Link href="/" className="font-mono text-sm font-semibold tracking-tight">
          {site.name}
        </Link>
        <nav aria-label="Main" className="hidden items-center gap-5 text-sm text-muted-foreground sm:flex">
          {site.nav.map((item) => (
            <Link key={item.href} href={item.href} className="hover:text-foreground">
              {item.label}
            </Link>
          ))}
          <a href={site.docsUrl} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 hover:text-foreground">
            Docs <ArrowUpRight className="size-3.5" aria-hidden />
          </a>
        </nav>
        <div className="ml-auto flex items-center gap-2">
          <ThemeToggle />
          <Button asChild size="sm">
            <Link href="/demo">Request demo</Link>
          </Button>
        </div>
      </div>
    </header>
  );
}
```

`web/components/site-footer.tsx`:

```tsx
import Link from "next/link";
import { site } from "@/content/site";

export function SiteFooter() {
  const year = new Date().getFullYear();
  return (
    <footer className="border-t border-border/60">
      <div className="mx-auto flex max-w-6xl flex-col gap-4 px-4 py-10 text-sm text-muted-foreground sm:flex-row sm:items-center sm:px-6">
        <nav aria-label="Footer" className="flex flex-wrap gap-5">
          {site.nav.map((item) => (
            <Link key={item.href} href={item.href} className="hover:text-foreground">
              {item.label}
            </Link>
          ))}
          <a href={site.docsUrl} className="hover:text-foreground">Docs</a>
          <a href={site.githubUrl} className="hover:text-foreground">GitHub</a>
          <a href={site.licenseUrl} className="hover:text-foreground">{site.licenseName}</a>
        </nav>
        <p className="sm:ml-auto">© {year} {site.name}</p>
      </div>
    </footer>
  );
}
```

The footer repeats the nav so small screens (where the header nav is hidden) still reach every page.

- [ ] **Step 6: Layout and fonts.** Replace `web/app/layout.tsx`:

```tsx
import type { Metadata } from "next";
import type { ReactNode } from "react";
import { GeistMono } from "geist/font/mono";
import { GeistSans } from "geist/font/sans";
import { SiteFooter } from "@/components/site-footer";
import { SiteHeader } from "@/components/site-header";
import { ThemeProvider } from "@/components/theme-provider";
import { site } from "@/content/site";
import "./globals.css";

export const metadata: Metadata = {
  title: site.name,
  description: site.description,
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning className={`${GeistSans.variable} ${GeistMono.variable}`}>
      <body className="min-h-dvh bg-background font-sans text-foreground antialiased">
        <ThemeProvider attribute="class" defaultTheme="dark" enableSystem={false} disableTransitionOnChange>
          <a
            href="#main"
            className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-50 focus:rounded focus:bg-background focus:px-3 focus:py-2"
          >
            Skip to content
          </a>
          <SiteHeader />
          <main id="main">{children}</main>
          <SiteFooter />
        </ThemeProvider>
      </body>
    </html>
  );
}
```

In `web/app/globals.css`, inside the `@theme inline { … }` block shadcn created, set the font tokens (add them if absent):

```css
  --font-sans: var(--font-geist-sans);
  --font-mono: var(--font-geist-mono);
```

Remove any `Geist`/`Geist_Mono` imports from `next/font/google` left by the starter.

- [ ] **Step 7: 404 page** — `web/app/not-found.tsx`:

```tsx
import Link from "next/link";
import { Button } from "@/components/ui/button";

export default function NotFound() {
  return (
    <section className="mx-auto flex max-w-2xl flex-col items-start gap-6 px-4 py-32 sm:px-6">
      <p className="font-mono text-sm text-muted-foreground">404</p>
      <h1 className="text-4xl font-semibold tracking-tight">Page not found</h1>
      <p className="text-muted-foreground">That page does not exist, or it moved.</p>
      <Button asChild variant="outline">
        <Link href="/">Back to home</Link>
      </Button>
    </section>
  );
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd web && pnpm lint && pnpm typecheck && pnpm test:e2e`
Expected: PASS (smoke + shell).

- [ ] **Step 9: Commit**

```bash
git add web
git commit -m "feat(web): site shell with header, footer, theme toggle and 404"
```

---

### Task 3: Home page

**Files:**
- Create: `web/content/home.ts`, `web/components/home/terminal.tsx`, `web/components/home/hero.tsx`, `web/e2e/home.spec.ts`
- Modify: `web/app/page.tsx`

**Interfaces:**
- Consumes: `site` from `@/content/site`; shadcn `Button`, `Card`.
- Produces: `home` content const; `<Hero />` (server component; CSS-only animation).

Read `.claude/skills/frontend-design` and `.claude/skills/ui-animation` before building the hero. The animation is CSS-only, runs only under `motion-safe:` (so reduced motion shows the final state), and must not delay the LCP element: the `<h1>` has no entrance animation.

- [ ] **Step 1: Write the failing test** — `web/e2e/home.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

test("home tells the story in order", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1, name: "One policy for every coding agent" })).toBeVisible();
  for (const name of ["What the vendor consoles leave uncovered", "How it works", "Governs the agents your developers use"]) {
    await expect(page.getByRole("heading", { level: 2, name })).toBeVisible();
  }
  for (const agent of ["Claude Code", "Codex", "Gemini CLI"]) {
    await expect(page.getByRole("heading", { level: 3, name: agent })).toBeVisible();
  }
});

test("hero shows real commands and policy, no fabricated output", async ({ page }) => {
  await page.goto("/");
  const terminal = page.getByRole("figure", { name: "Terminal" });
  await expect(terminal).toContainText("awd apply org-policy.yaml");
  await expect(terminal).toContainText("aw-sync install-timer");
  await expect(page.getByRole("figure", { name: "Policy" })).toContainText("allowManagedPermissionRulesOnly: true");
});

test("hero CTAs", async ({ page }) => {
  await page.goto("/");
  const main = page.getByRole("main");
  await expect(main.getByRole("link", { name: "Request demo" }).first()).toHaveAttribute("href", "/demo");
  await expect(main.getByRole("link", { name: "See how it works" })).toHaveAttribute("href", "/product");
});

test("reduced motion shows the final hero state", async ({ browser }) => {
  const context = await browser.newContext({ reducedMotion: "reduce" });
  const page = await context.newPage();
  await page.goto("/");
  await expect(page.getByRole("figure", { name: "Terminal" })).toContainText("claude");
  await context.close();
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && pnpm test:e2e e2e/home.spec.ts`
Expected: FAIL — heading text not found.

- [ ] **Step 3: Content** — `web/content/home.ts`. The policy snippet is an excerpt of the repo's real `examples/org-policy.yaml`; do not edit its keys.

```ts
export const home = {
  hero: {
    eyebrow: "Claude Code · Codex · Gemini CLI",
    title: "One policy for every coding agent",
    lede:
      "Write your organization's rules once. Every developer session gets the slice that applies to their groups and the repository they are in — applied at launch, even when nobody types the wrapper.",
    primaryCta: { href: "/demo", label: "Request demo" },
    secondaryCta: { href: "/product", label: "See how it works" },
    terminal: [
      { comment: "# on the control plane", command: "awd apply org-policy.yaml" },
      { comment: "# once per machine, as root", command: "aw-sync install-timer" },
      { comment: "# developers keep typing what they type", command: "claude" },
    ],
    policy: `rules:
  - name: baseline
    agents:
      claude:
        managed:
          permissions:
            deny:
              - Read(./.env)
          allowManagedPermissionRulesOnly: true
  - name: payments-repos
    match:
      repos: ["github.com/acme/payments*"]
    agents:
      claude:
        managed:
          permissions:
            deny:
              - Bash(curl *)`,
  },
  problem: {
    title: "What the vendor consoles leave uncovered",
    items: [
      {
        title: "Rules per repository",
        body: "Managed settings apply machine-wide. A payments repository and a sandbox get the same rules unless something targets the repository itself.",
      },
      {
        title: "Rules per group on seat plans",
        body: "Organizations on per-seat plans cannot give the platform team one policy and contractors another from the vendor console.",
      },
      {
        title: "More than one agent",
        body: "Teams run Claude Code, Codex and Gemini CLI side by side. Each has its own settings format, and none reads the others'.",
      },
    ],
  },
  steps: {
    title: "How it works",
    items: [
      {
        command: "awd",
        title: "Author one policy",
        body: "The control plane holds your rules, your groups and your SCIM-provisioned users, and signs a bundle for each enrolled machine.",
      },
      {
        command: "aw-sync",
        title: "Deliver it to every machine",
        body: "A root-owned timer fetches the machine's signed bundle and writes each agent's managed settings in the agent's own format.",
      },
      {
        command: "policyHelper",
        title: "Enforce it at launch",
        body: "Claude Code runs the helper on every start, including a bare `claude`, and the helper applies the rules for the repository the session is in.",
      },
    ],
  },
  agents: {
    title: "Governs the agents your developers use",
    items: [
      { name: "Claude Code", body: "Managed settings and a policy helper computed per repository at every launch." },
      { name: "Codex", body: "A machine-wide requirements.toml, plus repository rules through `aw codex`." },
      { name: "Gemini CLI", body: "System settings and an admin policy file, plus repository rules through `aw gemini`." },
    ],
  },
  closing: {
    title: "See it against your own policy",
    body: "Bring the rules you enforce today. We will show you how they map to groups and repositories.",
    cta: { href: "/demo", label: "Request demo" },
  },
} as const;
```

- [ ] **Step 4: Terminal and hero.** `web/components/home/terminal.tsx` (server component; used by the hero):

```tsx
import { home } from "@/content/home";

export function PolicySnippet() {
  return (
    <figure aria-label="Policy" className="overflow-hidden rounded-lg border border-border bg-card">
      <figcaption className="border-b border-border px-4 py-2 font-mono text-xs text-muted-foreground">org-policy.yaml</figcaption>
      <pre className="overflow-x-auto p-4 font-mono text-xs leading-relaxed"><code>{home.hero.policy}</code></pre>
    </figure>
  );
}
```

`web/components/home/hero.tsx`:

```tsx
import type { CSSProperties } from "react";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { PolicySnippet } from "@/components/home/terminal";
import { home } from "@/content/home";

export function Hero() {
  const { hero } = home;
  return (
    <section className="mx-auto grid max-w-6xl gap-12 px-4 pb-20 pt-16 sm:px-6 lg:grid-cols-2 lg:pt-24">
      <div className="flex flex-col gap-6">
        <p className="font-mono text-xs uppercase tracking-widest text-muted-foreground">{hero.eyebrow}</p>
        <h1 className="text-balance text-4xl font-semibold tracking-tight sm:text-5xl">{hero.title}</h1>
        <p className="text-pretty text-lg text-muted-foreground">{hero.lede}</p>
        <div className="flex flex-wrap gap-3">
          <Button asChild size="lg"><Link href={hero.primaryCta.href}>{hero.primaryCta.label}</Link></Button>
          <Button asChild size="lg" variant="outline"><Link href={hero.secondaryCta.href}>{hero.secondaryCta.label}</Link></Button>
        </div>
      </div>
      <div className="flex flex-col gap-4">
        <figure aria-label="Terminal" className="rounded-lg border border-border bg-card p-4 font-mono text-sm">
          {hero.terminal.map((line, i) => (
            <div
              key={line.command}
              className="motion-safe:animate-terminal-line"
              style={{ "--line-delay": `${300 + i * 500}ms` } as CSSProperties}
            >
              <div className="text-muted-foreground">{line.comment}</div>
              <div className="mb-2"><span className="text-muted-foreground" aria-hidden>$ </span>{line.command}</div>
            </div>
          ))}
        </figure>
        <PolicySnippet />
      </div>
    </section>
  );
}
```

Add to `web/app/globals.css` (Tailwind v4 registers `animate-terminal-line` from the `--animate-*` theme token):

```css
@theme {
  --animate-terminal-line: terminal-line 300ms ease-out var(--line-delay, 0ms) both;
}

@keyframes terminal-line {
  from { opacity: 0; transform: translateY(4px); }
  to { opacity: 1; transform: none; }
}
```

`both` fill mode holds the hidden first frame only while the delay runs and the final frame afterwards, with or without JavaScript; `motion-safe:` drops the animation entirely under reduced motion.

- [ ] **Step 5: Page** — replace `web/app/page.tsx`:

```tsx
import Link from "next/link";
import { Hero } from "@/components/home/hero";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { home } from "@/content/home";

export default function HomePage() {
  return (
    <>
      <Hero />
      <section className="mx-auto max-w-6xl px-4 py-16 sm:px-6">
        <h2 className="text-2xl font-semibold tracking-tight">{home.problem.title}</h2>
        <div className="mt-8 grid gap-4 md:grid-cols-3">
          {home.problem.items.map((item) => (
            <Card key={item.title}>
              <CardHeader><CardTitle className="text-base">{item.title}</CardTitle></CardHeader>
              <CardContent className="text-sm text-muted-foreground">{item.body}</CardContent>
            </Card>
          ))}
        </div>
      </section>
      <section className="mx-auto max-w-6xl px-4 py-16 sm:px-6">
        <h2 className="text-2xl font-semibold tracking-tight">{home.steps.title}</h2>
        <ol className="mt-8 grid gap-6 md:grid-cols-3">
          {home.steps.items.map((step, i) => (
            <li key={step.command} className="flex flex-col gap-2">
              <span className="font-mono text-xs text-muted-foreground">{String(i + 1).padStart(2, "0")} · {step.command}</span>
              <h3 className="font-medium">{step.title}</h3>
              <p className="text-sm text-muted-foreground">{step.body}</p>
            </li>
          ))}
        </ol>
      </section>
      <section className="mx-auto max-w-6xl px-4 py-16 sm:px-6">
        <h2 className="text-2xl font-semibold tracking-tight">{home.agents.title}</h2>
        <div className="mt-8 grid gap-4 md:grid-cols-3">
          {home.agents.items.map((agent) => (
            <div key={agent.name} className="rounded-lg border border-border p-5">
              <h3 className="font-medium">{agent.name}</h3>
              <p className="mt-2 text-sm text-muted-foreground">{agent.body}</p>
            </div>
          ))}
        </div>
      </section>
      <section className="mx-auto max-w-6xl px-4 py-20 sm:px-6">
        <div className="flex flex-col items-start gap-4 rounded-xl border border-border bg-card p-8">
          <h2 className="text-2xl font-semibold tracking-tight">{home.closing.title}</h2>
          <p className="text-muted-foreground">{home.closing.body}</p>
          <Button asChild><Link href={home.closing.cta.href}>{home.closing.cta.label}</Link></Button>
        </div>
      </section>
    </>
  );
}
```

Inline backtick spans in content (e.g. `` `claude` ``) render literally; that is acceptable here — do not add a markdown renderer. Improve spacing, colour and typography following the frontend-design skill, but keep every heading level and text string the tests assert.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd web && pnpm lint && pnpm typecheck && pnpm test:e2e`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web
git commit -m "feat(web): home page with hero, problem, how-it-works and agents"
```

---

### Task 4: Product and pricing pages

**Files:**
- Create: `web/content/product.ts`, `web/content/pricing.ts`, `web/app/product/page.tsx`, `web/app/pricing/page.tsx`, `web/e2e/product-pricing.spec.ts`

**Interfaces:**
- Consumes: `site`; shadcn `Badge`, `Button`, `Card`, `Table`.
- Produces: `product` and `pricing` content consts; `pricing.faq: readonly { q: string; a: string }[]` (Task 7 builds `FAQPage` JSON-LD from it); `PlanId = "team" | "enterprise" | "self-hosted"` exported from `@/content/pricing` (Task 5 imports it).

- [ ] **Step 1: Write the failing test** — `web/e2e/product-pricing.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

test("product lists shipped capabilities and badges the console", async ({ page }) => {
  await page.goto("/product");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  for (const name of [
    "Group and repository targeting",
    "Signed bundles",
    "SCIM provisioning",
    "Scheduled sync on every OS",
    "Claude Code always starts",
    "aw doctor",
    "Hosted console",
  ]) {
    await expect(page.getByRole("heading", { level: 2, name })).toBeVisible();
  }
  const console = page.getByRole("region", { name: "Hosted console" });
  await expect(console.getByText("Coming soon")).toBeVisible();
});

test("pricing shows three tiers, all Contact us, with plan-specific CTAs", async ({ page }) => {
  await page.goto("/pricing");
  for (const [tier, plan] of [["Team", "team"], ["Enterprise", "enterprise"], ["Self-hosted", "self-hosted"]] as const) {
    const card = page.getByRole("article", { name: tier });
    await expect(card.getByText("Contact us")).toBeVisible();
    await expect(card.getByRole("link", { name: /Request demo|Talk to us/ })).toHaveAttribute("href", `/demo?plan=${plan}`);
  }
  await expect(page.getByRole("article", { name: "Team" }).getByText("Coming soon")).toBeVisible();
  await expect(page.getByText(/\$\s?\d/)).toHaveCount(0);
});

test("pricing FAQ answers are in the page", async ({ page }) => {
  await page.goto("/pricing");
  const faq = page.getByRole("region", { name: "Frequently asked questions" });
  await expect(faq.locator("details")).toHaveCount(6);
  await faq.getByText("What happens if the control plane is down?").click();
  await expect(faq.getByText(/last signed bundle/)).toBeVisible();
});

test("header navigation reaches both pages", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("banner").getByRole("link", { name: "Product" }).click();
  await expect(page).toHaveURL(/\/product$/);
  await page.getByRole("banner").getByRole("link", { name: "Pricing" }).click();
  await expect(page).toHaveURL(/\/pricing$/);
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && pnpm test:e2e e2e/product-pricing.spec.ts`
Expected: FAIL — 404s.

- [ ] **Step 3: Content** — `web/content/product.ts`. Every claim here is shipped; do not add features.

```ts
export const product = {
  title: "Govern coding agents like the rest of your fleet",
  lede: "Agentic Warden is a control plane, a sync agent and a launch-time helper. Together they turn one policy into the right settings for every developer, repository and agent.",
  features: [
    {
      id: "targeting",
      title: "Group and repository targeting",
      body: "Rules match on identity-provider groups and on repository globs. Later rules override earlier ones per key, and permission lists union, so precedence is the order you write.",
    },
    {
      id: "signed-bundles",
      title: "Signed bundles",
      body: "The control plane signs each machine's bundle. The sync agent verifies the signature before writing anything, so a tampered bundle never reaches an agent.",
    },
    {
      id: "scim",
      title: "SCIM provisioning",
      body: "A SCIM 2.0 endpoint accepts users and groups from Okta and Microsoft Entra ID. Provisioned groups join authored groups and snapshots when rules are resolved.",
    },
    {
      id: "sync",
      title: "Scheduled sync on every OS",
      body: "`aw-sync install-timer` installs a systemd timer on Linux, a launchd daemon on macOS or a scheduled task on Windows, and refuses to schedule a binary a non-root user could replace.",
    },
    {
      id: "fail-safe",
      title: "Claude Code always starts",
      body: "The policy helper works offline from the last synced bundle, validates its output against Claude Code's settings schema and never exits non-zero, so a bad day on the network never blocks a developer.",
    },
    {
      id: "doctor",
      title: "aw doctor",
      body: "One command shows each agent's compiled policy, which repository it was compiled for, when the machine last synced, and any setting that shadows the managed ones.",
    },
  ],
  console: {
    id: "console",
    title: "Hosted console",
    badge: "Coming soon",
    body: "Manage policy, groups and enrolled machines from a browser, without running the control plane yourself.",
    cta: { href: "/demo?plan=team", label: "Join early access" },
  },
} as const;
```

`web/content/pricing.ts`:

```ts
export type PlanId = "team" | "enterprise" | "self-hosted";

export const planIds: readonly PlanId[] = ["team", "enterprise", "self-hosted"];

export const pricing = {
  title: "Pricing",
  lede: "Every plan starts with a conversation about the agents and rules you run today.",
  tiers: [
    {
      id: "team",
      name: "Team",
      badge: "Coming soon",
      summary: "Hosted for you. For teams that want governance without running a control plane.",
      price: "Contact us",
      features: ["Hosted console", "Claude Code, Codex and Gemini CLI", "Group and repository targeting", "Signed bundles"],
      cta: "Request demo",
    },
    {
      id: "enterprise",
      name: "Enterprise",
      badge: null,
      summary: "For organizations provisioning users from an identity provider.",
      price: "Contact us",
      features: ["Everything in Team", "SCIM provisioning (Okta, Entra ID)", "Rollout help for your policy", "Hosted or self-hosted"],
      cta: "Request demo",
    },
    {
      id: "self-hosted",
      name: "Self-hosted",
      badge: null,
      summary: "Apache-2.0 source. Run the control plane on your own infrastructure.",
      price: "Contact us",
      features: ["Control plane on your servers", "PostgreSQL storage", "Every agent and targeting feature", "Talk to us for rollout help"],
      cta: "Talk to us",
    },
  ] satisfies readonly {
    id: PlanId;
    name: string;
    badge: string | null;
    summary: string;
    price: string;
    features: readonly string[];
    cta: string;
  }[],
  comparison: {
    columns: ["Team", "Enterprise", "Self-hosted"],
    rows: [
      { feature: "Claude Code, Codex, Gemini CLI", values: ["Yes", "Yes", "Yes"] },
      { feature: "Group and repository targeting", values: ["Yes", "Yes", "Yes"] },
      { feature: "Signed bundles", values: ["Yes", "Yes", "Yes"] },
      { feature: "SCIM provisioning", values: ["—", "Yes", "Yes"] },
      { feature: "Hosted console", values: ["Coming soon", "Coming soon", "—"] },
      { feature: "Runs on your infrastructure", values: ["—", "Optional", "Yes"] },
    ],
  },
  faq: [
    {
      q: "Does it replace Claude Code's managed settings?",
      a: "No. It builds on them. The sync agent writes the managed-settings drop-in that names the policy helper, and the helper computes the settings for each session.",
    },
    {
      q: "What happens if the control plane is down?",
      a: "Nothing changes for developers. The helper reads the last signed bundle on disk, so Claude Code keeps starting with the last policy it received.",
    },
    {
      q: "Can a developer bypass it by running claude directly?",
      a: "Not for Claude Code: it runs the policy helper on every launch, wrapper or not. For Codex and Gemini CLI the machine-wide files always apply; repository-specific rules apply when launched through aw codex or aw gemini.",
    },
    {
      q: "Which identity providers work?",
      a: "Any SCIM 2.0 client. The endpoint handles the request shapes Okta and Microsoft Entra ID send.",
    },
    {
      q: "Is the hosted console available?",
      a: "Not yet. Choose Team or pick hosted on the demo form and we will contact you when early access opens.",
    },
    {
      q: "How much does it cost?",
      a: "Pricing depends on the plan and the size of your organization. Request a demo and we will send a quote.",
    },
  ],
} as const;
```

- [ ] **Step 4: Pages.** `web/app/product/page.tsx`:

```tsx
import Link from "next/link";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { product } from "@/content/product";

export default function ProductPage() {
  return (
    <div className="mx-auto max-w-4xl px-4 py-16 sm:px-6">
      <h1 className="text-balance text-4xl font-semibold tracking-tight">{product.title}</h1>
      <p className="mt-4 text-lg text-muted-foreground">{product.lede}</p>
      <div className="mt-12 flex flex-col gap-12">
        {product.features.map((f) => (
          <section key={f.id} id={f.id} aria-labelledby={`${f.id}-title`}>
            <h2 id={`${f.id}-title`} className="text-xl font-semibold">{f.title}</h2>
            <p className="mt-2 text-muted-foreground">{f.body}</p>
          </section>
        ))}
        <section
          id={product.console.id}
          aria-labelledby="console-title"
          className="rounded-xl border border-border bg-card p-6"
        >
          <div className="flex items-center gap-3">
            <h2 id="console-title" className="text-xl font-semibold">{product.console.title}</h2>
            <Badge variant="secondary">{product.console.badge}</Badge>
          </div>
          <p className="mt-2 text-muted-foreground">{product.console.body}</p>
          <Button asChild className="mt-4" variant="outline">
            <Link href={product.console.cta.href}>{product.console.cta.label}</Link>
          </Button>
        </section>
      </div>
    </div>
  );
}
```

`web/app/pricing/page.tsx`:

```tsx
import Link from "next/link";
import { Check } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { pricing } from "@/content/pricing";

export default function PricingPage() {
  return (
    <div className="mx-auto max-w-6xl px-4 py-16 sm:px-6">
      <h1 className="text-4xl font-semibold tracking-tight">{pricing.title}</h1>
      <p className="mt-4 text-lg text-muted-foreground">{pricing.lede}</p>

      <div className="mt-12 grid gap-6 md:grid-cols-3">
        {pricing.tiers.map((tier) => (
          <article key={tier.id} aria-labelledby={`tier-${tier.id}`} className="flex flex-col gap-4 rounded-xl border border-border bg-card p-6">
            <div className="flex items-center gap-2">
              <h2 id={`tier-${tier.id}`} className="text-lg font-semibold">{tier.name}</h2>
              {tier.badge ? <Badge variant="secondary">{tier.badge}</Badge> : null}
            </div>
            <p className="text-sm text-muted-foreground">{tier.summary}</p>
            <p className="text-2xl font-semibold">{tier.price}</p>
            <ul className="flex flex-col gap-2 text-sm">
              {tier.features.map((feature) => (
                <li key={feature} className="flex gap-2"><Check className="mt-0.5 size-4 shrink-0" aria-hidden />{feature}</li>
              ))}
            </ul>
            <Button asChild className="mt-auto">
              <Link href={`/demo?plan=${tier.id}`}>{tier.cta}</Link>
            </Button>
          </article>
        ))}
      </div>

      <section aria-labelledby="compare-title" className="mt-20">
        <h2 id="compare-title" className="text-2xl font-semibold tracking-tight">Compare plans</h2>
        <div className="mt-6 overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">Feature</TableHead>
                {pricing.comparison.columns.map((c) => <TableHead key={c} scope="col">{c}</TableHead>)}
              </TableRow>
            </TableHeader>
            <TableBody>
              {pricing.comparison.rows.map((row) => (
                <TableRow key={row.feature}>
                  <TableHead scope="row" className="font-normal">{row.feature}</TableHead>
                  {row.values.map((v, i) => <TableCell key={i}>{v}</TableCell>)}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </section>

      <section aria-labelledby="faq-title" className="mt-20 max-w-3xl">
        <h2 id="faq-title" className="text-2xl font-semibold tracking-tight">Frequently asked questions</h2>
        <div className="mt-6 divide-y divide-border border-y border-border">
          {pricing.faq.map((item) => (
            <details key={item.q} className="group py-4">
              <summary className="cursor-pointer font-medium">{item.q}</summary>
              <p className="mt-2 text-muted-foreground">{item.a}</p>
            </details>
          ))}
        </div>
      </section>
    </div>
  );
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd web && pnpm lint && pnpm typecheck && pnpm test:e2e`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web
git commit -m "feat(web): product and pricing pages with coming-soon console and FAQ"
```

---

### Task 5: Demo request domain — schema, email, sender, submit

**Files:**
- Create: `web/lib/demo/schema.ts`, `web/lib/demo/email.ts`, `web/lib/demo/sender.ts`, `web/lib/demo/submit.ts`, and tests `web/lib/demo/schema.test.ts`, `web/lib/demo/email.test.ts`, `web/lib/demo/submit.test.ts`, `web/lib/demo/sender.test.ts`

**Interfaces:**
- Consumes: `PlanId`, `planIds` from `@/content/pricing`; `Env` from `@/lib/env`.
- Produces (Task 6 relies on these exactly):
  - `demoSchema` (zod object), `type DemoRequest = z.infer<typeof demoSchema>`
  - `sizeOptions`, `agentOptions`, `deploymentOptions`: `readonly { value: string; label: string }[]`
  - `isWebmail(email: string): boolean`
  - `parsePlan(value: unknown): PlanId | undefined`
  - `formatEmail(req: DemoRequest): { subject: string; text: string }`
  - `type Sender = (msg: { to: string; from: string; replyTo: string; subject: string; text: string }) => Promise<void>`
  - `class SendError extends Error { status?: number }`
  - `senderFromEnv(env?: Env): Sender | null` — `null` when not configured
  - `type DemoState = { status: "idle" } | { status: "ok" } | { status: "invalid"; fieldErrors: Partial<Record<DemoField, string>>; values: Record<string, string | string[]> } | { status: "failed"; inbox: string | null; values: Record<string, string | string[]> }`
  - `type DemoField = "name" | "email" | "company" | "size" | "agents" | "deployment" | "plan" | "message"`
  - `submitDemo(form: FormData, deps: SubmitDeps): Promise<DemoState>` with `type SubmitDeps = { send: Sender | null; inbox: string | null; from: string | null; now: () => number; log: (msg: string, detail: Record<string, unknown>) => void }`
  - Form field names: `name`, `email`, `company`, `size`, `agents` (repeated), `deployment`, `plan`, `message`, honeypot `website`, timestamp `startedAt` (ms since epoch).

- [ ] **Step 1: Write the failing schema test** — `web/lib/demo/schema.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { demoSchema, isWebmail, parsePlan } from "./schema";

const valid = {
  name: "Ada Lovelace",
  email: "ada@acme.com",
  company: "Acme",
  size: "51-500",
  agents: ["claude-code", "codex"],
  deployment: "hosted",
};

describe("demoSchema", () => {
  it("accepts a minimal valid request", () => {
    expect(demoSchema.safeParse(valid).success).toBe(true);
  });
  it("accepts optional plan and message", () => {
    expect(demoSchema.safeParse({ ...valid, plan: "team", message: "hi" }).success).toBe(true);
  });
  it.each([
    ["name", ""],
    ["name", "x".repeat(101)],
    ["email", "not-an-email"],
    ["email", `${"a".repeat(250)}@x.io`],
    ["company", ""],
    ["company", "x".repeat(101)],
    ["size", "10"],
    ["agents", []],
    ["agents", ["cursor"]],
    ["deployment", "cloud"],
    ["plan", "free"],
    ["message", "x".repeat(2001)],
  ])("rejects %s = %j", (field, value) => {
    const result = demoSchema.safeParse({ ...valid, [field]: value });
    expect(result.success).toBe(false);
    expect(result.error?.issues[0]?.path[0]).toBe(field);
  });
  it("trims text fields", () => {
    const result = demoSchema.parse({ ...valid, name: "  Ada  " });
    expect(result.name).toBe("Ada");
  });
});

describe("isWebmail", () => {
  it.each(["a@gmail.com", "a@Outlook.com", "a@yahoo.com", "a@icloud.com", "a@proton.me"])("flags %s", (email) => {
    expect(isWebmail(email)).toBe(true);
  });
  it("does not flag a company domain", () => {
    expect(isWebmail("a@acme.com")).toBe(false);
  });
});

describe("parsePlan", () => {
  it("keeps known plans", () => {
    expect(parsePlan("enterprise")).toBe("enterprise");
  });
  it("ignores unknown values", () => {
    expect(parsePlan("free")).toBeUndefined();
    expect(parsePlan(["team"])).toBeUndefined();
    expect(parsePlan(undefined)).toBeUndefined();
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && pnpm test lib/demo/schema.test.ts`
Expected: FAIL — cannot resolve `./schema`.

- [ ] **Step 3: Implement** `web/lib/demo/schema.ts`:

```ts
import { z } from "zod";
import { planIds, type PlanId } from "@/content/pricing";

export const sizeOptions = [
  { value: "1-50", label: "1–50" },
  { value: "51-500", label: "51–500" },
  { value: "500+", label: "500+" },
] as const;

export const agentOptions = [
  { value: "claude-code", label: "Claude Code" },
  { value: "codex", label: "Codex" },
  { value: "gemini-cli", label: "Gemini CLI" },
  { value: "other", label: "Other" },
] as const;

export const deploymentOptions = [
  { value: "self-hosted", label: "Self-hosted" },
  { value: "hosted", label: "Hosted by us" },
  { value: "unsure", label: "Not sure yet" },
] as const;

const values = <T extends readonly { value: string }[]>(opts: T) =>
  opts.map((o) => o.value) as [T[number]["value"], ...T[number]["value"][]];

export const demoSchema = z.object({
  name: z.string().trim().min(1, "Enter your name.").max(100, "Use at most 100 characters."),
  email: z.string().trim().max(254, "Use at most 254 characters.").email("Enter a valid email address."),
  company: z.string().trim().min(1, "Enter your company.").max(100, "Use at most 100 characters."),
  size: z.enum(values(sizeOptions), { message: "Choose a company size." }),
  agents: z.array(z.enum(values(agentOptions))).min(1, "Choose at least one agent."),
  deployment: z.enum(values(deploymentOptions), { message: "Choose a deployment." }),
  plan: z.enum(planIds as [PlanId, ...PlanId[]]).optional(),
  message: z.string().trim().max(2000, "Use at most 2000 characters.").optional(),
});

export type DemoRequest = z.infer<typeof demoSchema>;

const WEBMAIL = new Set(["gmail.com", "outlook.com", "yahoo.com", "icloud.com", "proton.me"]);

export function isWebmail(email: string): boolean {
  const domain = email.split("@").pop()?.trim().toLowerCase() ?? "";
  return WEBMAIL.has(domain);
}

export function parsePlan(value: unknown): PlanId | undefined {
  return typeof value === "string" && (planIds as readonly string[]).includes(value)
    ? (value as PlanId)
    : undefined;
}
```

If zod 4 reports the email max-length error path differently, keep the test's expectation (`path[0] === "email"`) and adjust the chain, not the test. `z.string().email()` is deprecated in zod 4 in favour of `z.email()`; use whichever form keeps `.trim()` and `.max()` working and emits no deprecation warning.

- [ ] **Step 4: Run it to verify it passes**

Run: `cd web && pnpm test lib/demo/schema.test.ts`
Expected: PASS.

- [ ] **Step 5: Write the failing email test** — `web/lib/demo/email.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { formatEmail } from "./email";

const req = {
  name: "Ada",
  email: "ada@acme.com",
  company: "Acme",
  size: "51-500" as const,
  agents: ["claude-code" as const, "gemini-cli" as const],
  deployment: "hosted" as const,
};

describe("formatEmail", () => {
  it("names the company and plan in the subject", () => {
    expect(formatEmail({ ...req, plan: "team" }).subject).toBe("Demo request: Acme (team)");
    expect(formatEmail(req).subject).toBe('Demo request: Acme (no plan)');
  });
  it("lists every field in the body with labels", () => {
    const { text } = formatEmail({ ...req, message: "Line one\nLine two" });
    expect(text).toContain("Name: Ada");
    expect(text).toContain("Email: ada@acme.com");
    expect(text).toContain("Company size: 51–500");
    expect(text).toContain("Agents: Claude Code, Gemini CLI");
    expect(text).toContain("Deployment: Hosted by us");
    expect(text).toContain("Line one\nLine two");
  });
  it("strips newlines from the subject", () => {
    expect(formatEmail({ ...req, company: "Acme\r\nBcc: x@y.z" }).subject).toBe("Demo request: Acme  Bcc: x@y.z (no plan)");
  });
});
```

- [ ] **Step 6: Run it to verify it fails**

Run: `cd web && pnpm test lib/demo/email.test.ts`
Expected: FAIL — cannot resolve `./email`.

- [ ] **Step 7: Implement** `web/lib/demo/email.ts`:

```ts
import { agentOptions, deploymentOptions, sizeOptions, type DemoRequest } from "./schema";

const label = (opts: readonly { value: string; label: string }[], value: string) =>
  opts.find((o) => o.value === value)?.label ?? value;

const oneLine = (s: string) => s.replace(/[\r\n]/g, " ");

export function formatEmail(req: DemoRequest): { subject: string; text: string } {
  const subject = oneLine(`Demo request: ${req.company} (${req.plan ?? "no plan"})`);
  const lines = [
    `Name: ${req.name}`,
    `Email: ${req.email}`,
    `Company: ${req.company}`,
    `Company size: ${label(sizeOptions, req.size)}`,
    `Agents: ${req.agents.map((a) => label(agentOptions, a)).join(", ")}`,
    `Deployment: ${label(deploymentOptions, req.deployment)}`,
    `Plan: ${req.plan ?? "none"}`,
    "",
    "Message:",
    req.message || "(none)",
  ];
  return { subject, text: lines.join("\n") };
}
```

- [ ] **Step 8: Run it to verify it passes**

Run: `cd web && pnpm test lib/demo/email.test.ts`
Expected: PASS.

- [ ] **Step 9: Write the failing sender test** — `web/lib/demo/sender.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { SendError, fakeSender, senderFromEnv } from "./sender";

const msg = { to: "in@x.test", from: "site@x.test", replyTo: "ada@acme.com", subject: "s", text: "t" };

describe("senderFromEnv", () => {
  it("is null without a Resend key", () => {
    expect(senderFromEnv({})).toBeNull();
  });
  it("returns the fake when DEMO_SENDER=fake", () => {
    expect(senderFromEnv({ DEMO_SENDER: "fake" })).toBe(fakeSender);
  });
  it("returns a Resend sender when a key is set", () => {
    expect(typeof senderFromEnv({ RESEND_API_KEY: "re_x" })).toBe("function");
  });
});

describe("fakeSender", () => {
  it("succeeds normally", async () => {
    await expect(fakeSender(msg)).resolves.toBeUndefined();
  });
  it("fails when the subject names the __fail__ company", async () => {
    await expect(fakeSender({ ...msg, subject: "Demo request: __fail__ (no plan)" })).rejects.toBeInstanceOf(SendError);
  });
});
```

- [ ] **Step 10: Run it to verify it fails**

Run: `cd web && pnpm test lib/demo/sender.test.ts`
Expected: FAIL — cannot resolve `./sender`.

- [ ] **Step 11: Implement** `web/lib/demo/sender.ts`:

```ts
import { Resend } from "resend";
import type { Env } from "@/lib/env";

export type Sender = (msg: {
  to: string;
  from: string;
  replyTo: string;
  subject: string;
  text: string;
}) => Promise<void>;

export class SendError extends Error {
  constructor(
    message: string,
    readonly status?: number,
  ) {
    super(message);
    this.name = "SendError";
  }
}

// Test-only: selected by DEMO_SENDER=fake, which assertBuildEnv forbids in
// production. Fails for the company "__fail__" so e2e tests can reach the
// fallback path.
export const fakeSender: Sender = async (msg) => {
  if (msg.subject.includes("Demo request: __fail__ ")) {
    throw new SendError("fake sender failure", 503);
  }
};

function resendSender(apiKey: string): Sender {
  const resend = new Resend(apiKey);
  return async ({ to, from, replyTo, subject, text }) => {
    const { error } = await resend.emails.send({ to, from, replyTo, subject, text });
    if (error) {
      const status = "statusCode" in error && typeof error.statusCode === "number" ? error.statusCode : undefined;
      throw new SendError(error.name ?? "resend_error", status);
    }
  };
}

export function senderFromEnv(env: Env = process.env): Sender | null {
  if (env.DEMO_SENDER === "fake") return fakeSender;
  if (env.RESEND_API_KEY) return resendSender(env.RESEND_API_KEY);
  return null;
}
```

Zod 4 prefers `{ error: "…" }` over `{ message: "…" }` for enum options; use whichever the installed version accepts without a deprecation warning, keeping the message text. Check the installed `resend` version's `emails.send` option name for reply-to (`replyTo` in v4+) and its error object's fields; adapt the property reads so `pnpm typecheck` passes, keeping the thrown `SendError(name, status)` shape.

- [ ] **Step 12: Run it to verify it passes**

Run: `cd web && pnpm test lib/demo/sender.test.ts`
Expected: PASS.

- [ ] **Step 13: Write the failing submit test** — `web/lib/demo/submit.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { SendError, type Sender } from "./sender";
import { submitDemo, type SubmitDeps } from "./submit";

const T0 = 1_000_000;

function form(overrides: Record<string, string | string[] | null> = {}): FormData {
  const base: Record<string, string | string[]> = {
    name: "Ada",
    email: "ada@acme.com",
    company: "Acme",
    size: "51-500",
    agents: ["claude-code", "codex"],
    deployment: "hosted",
    plan: "team",
    message: "",
    website: "",
    startedAt: String(T0),
  };
  const fd = new FormData();
  for (const [k, v] of Object.entries({ ...base, ...overrides })) {
    if (v === null) continue;
    for (const item of Array.isArray(v) ? v : [v]) fd.append(k, item);
  }
  return fd;
}

function deps(over: Partial<SubmitDeps> = {}): SubmitDeps & { sent: Parameters<Sender>[0][]; logs: unknown[][] } {
  const sent: Parameters<Sender>[0][] = [];
  const logs: unknown[][] = [];
  return {
    send: async (m) => { sent.push(m); },
    inbox: "sales@example.test",
    from: "site@example.test",
    now: () => T0 + 10_000,
    log: (msg, detail) => { logs.push([msg, detail]); },
    ...over,
    sent,
    logs,
  };
}

describe("submitDemo", () => {
  it("sends one email and returns ok", async () => {
    const d = deps();
    expect(await submitDemo(form(), d)).toEqual({ status: "ok" });
    expect(d.sent).toHaveLength(1);
    expect(d.sent[0]).toMatchObject({
      to: "sales@example.test",
      from: "site@example.test",
      replyTo: "ada@acme.com",
      subject: "Demo request: Acme (team)",
    });
  });

  it("returns field errors and the entered values when invalid", async () => {
    const d = deps();
    const state = await submitDemo(form({ email: "nope", agents: null }), d);
    expect(state.status).toBe("invalid");
    if (state.status !== "invalid") throw new Error("unreachable");
    expect(Object.keys(state.fieldErrors).sort()).toEqual(["agents", "email"]);
    expect(state.values.email).toBe("nope");
    expect(state.values.company).toBe("Acme");
    expect(d.sent).toHaveLength(0);
  });

  it("drops an unknown plan instead of failing", async () => {
    const d = deps();
    expect(await submitDemo(form({ plan: "free" }), d)).toEqual({ status: "ok" });
    expect(d.sent[0]?.subject).toBe("Demo request: Acme (no plan)");
  });

  it("silently drops a filled honeypot", async () => {
    const d = deps();
    expect(await submitDemo(form({ website: "http://spam" }), d)).toEqual({ status: "ok" });
    expect(d.sent).toHaveLength(0);
  });

  it("silently drops a submission under 3 seconds", async () => {
    const d = deps({ now: () => T0 + 2_999 });
    expect(await submitDemo(form(), d)).toEqual({ status: "ok" });
    expect(d.sent).toHaveLength(0);
  });

  it("accepts exactly 3 seconds", async () => {
    const d = deps({ now: () => T0 + 3_000 });
    await submitDemo(form(), d);
    expect(d.sent).toHaveLength(1);
  });

  it("skips the time check when the timestamp is missing or garbage", async () => {
    for (const startedAt of [null, "", "abc"]) {
      const d = deps();
      await submitDemo(form({ startedAt }), d);
      expect(d.sent).toHaveLength(1);
    }
  });

  it("fails with the inbox and values when the sender throws, logging no field values", async () => {
    const d = deps({ send: async () => { throw new SendError("rate_limit_exceeded", 429); } });
    const state = await submitDemo(form(), d);
    expect(state).toMatchObject({ status: "failed", inbox: "sales@example.test" });
    if (state.status !== "failed") throw new Error("unreachable");
    expect(state.values.name).toBe("Ada");
    expect(d.logs).toEqual([["demo: send failed", { error: "SendError", status: 429 }]]);
    const logged = JSON.stringify(d.logs);
    for (const secret of ["Ada", "ada@acme.com", "Acme"]) expect(logged).not.toContain(secret);
  });

  it("fails when the sender is not configured", async () => {
    const log = vi.fn();
    const state = await submitDemo(form(), deps({ send: null, log }));
    expect(state.status).toBe("failed");
    expect(log).toHaveBeenCalledWith("demo: sender not configured", {});
  });

  it("fails when inbox or from is not configured", async () => {
    expect((await submitDemo(form(), deps({ inbox: null }))).status).toBe("failed");
    expect((await submitDemo(form(), deps({ from: null }))).status).toBe("failed");
  });
});
```

- [ ] **Step 14: Run it to verify it fails**

Run: `cd web && pnpm test lib/demo/submit.test.ts`
Expected: FAIL — cannot resolve `./submit`.

- [ ] **Step 15: Implement** `web/lib/demo/submit.ts`:

```ts
import { formatEmail } from "./email";
import { demoSchema, parsePlan } from "./schema";
import type { Sender } from "./sender";

export type DemoField = "name" | "email" | "company" | "size" | "agents" | "deployment" | "plan" | "message";

type Values = Record<string, string | string[]>;

export type DemoState =
  | { status: "idle" }
  | { status: "ok" }
  | { status: "invalid"; fieldErrors: Partial<Record<DemoField, string>>; values: Values }
  | { status: "failed"; inbox: string | null; values: Values };

export type SubmitDeps = {
  send: Sender | null;
  inbox: string | null;
  from: string | null;
  now: () => number;
  log: (msg: string, detail: Record<string, unknown>) => void;
};

const MIN_FILL_MS = 3_000;
const TEXT_FIELDS = ["name", "email", "company", "size", "deployment", "plan", "message"] as const;

function readValues(form: FormData): Values {
  const values: Values = {};
  for (const key of TEXT_FIELDS) {
    const v = form.get(key);
    values[key] = typeof v === "string" ? v : "";
  }
  values.agents = form.getAll("agents").filter((v): v is string => typeof v === "string");
  return values;
}

function tooFast(form: FormData, now: number): boolean {
  const raw = form.get("startedAt");
  const started = typeof raw === "string" && raw !== "" ? Number(raw) : NaN;
  // A missing or unreadable timestamp skips the check; the honeypot still applies.
  return Number.isFinite(started) && now - started < MIN_FILL_MS;
}

export async function submitDemo(form: FormData, deps: SubmitDeps): Promise<DemoState> {
  const honeypot = form.get("website");
  if ((typeof honeypot === "string" && honeypot !== "") || tooFast(form, deps.now())) {
    return { status: "ok" };
  }

  const values = readValues(form);
  const parsed = demoSchema.safeParse({
    ...values,
    plan: parsePlan(values.plan),
    message: values.message || undefined,
  });
  if (!parsed.success) {
    const fieldErrors: Partial<Record<DemoField, string>> = {};
    for (const issue of parsed.error.issues) {
      const field = issue.path[0] as DemoField;
      fieldErrors[field] ??= issue.message;
    }
    return { status: "invalid", fieldErrors, values };
  }

  if (!deps.send || !deps.inbox || !deps.from) {
    deps.log("demo: sender not configured", {});
    return { status: "failed", inbox: deps.inbox, values };
  }

  const { subject, text } = formatEmail(parsed.data);
  try {
    await deps.send({ to: deps.inbox, from: deps.from, replyTo: parsed.data.email, subject, text });
  } catch (err) {
    const status = err && typeof err === "object" && "status" in err ? (err as { status?: unknown }).status : undefined;
    deps.log("demo: send failed", {
      error: err instanceof Error ? err.name : "unknown",
      status: typeof status === "number" ? status : undefined,
    });
    return { status: "failed", inbox: deps.inbox, values };
  }
  return { status: "ok" };
}
```

- [ ] **Step 16: Run all unit tests**

Run: `cd web && pnpm test && pnpm typecheck && pnpm lint`
Expected: PASS.

- [ ] **Step 17: Commit**

```bash
git add web/lib/demo
git commit -m "feat(web): demo request schema, email formatting, sender and submit logic"
```

---

### Task 6: Demo page, server action and form

**Files:**
- Create: `web/app/demo/page.tsx`, `web/app/demo/actions.ts`, `web/components/demo/demo-form.tsx`, `web/e2e/demo.spec.ts`

**Interfaces:**
- Consumes (Task 5): `submitDemo`, `DemoState`, `DemoField`, `senderFromEnv`, `sizeOptions`, `agentOptions`, `deploymentOptions`, `isWebmail`, `parsePlan`; `PlanId`, `pricing` from `@/content/pricing`.
- Produces: `submitDemoAction(prev: DemoState, form: FormData): Promise<DemoState>` (server action); `<DemoForm plan={PlanId | undefined} startedAt={number} />`.

- [ ] **Step 1: Write the failing e2e test** — `web/e2e/demo.spec.ts`:

```ts
import { expect, test, type Page } from "@playwright/test";

async function fill(page: Page, company = "Acme") {
  await page.getByLabel("Name").fill("Ada Lovelace");
  await page.getByLabel("Work email").fill("ada@acme.com");
  await page.getByLabel("Company", { exact: true }).fill(company);
  await page.getByLabel("Company size").selectOption("51-500");
  await page.getByRole("checkbox", { name: "Claude Code" }).check();
  await page.getByRole("radio", { name: "Hosted by us" }).check();
}

// The server drops submissions made within 3 s of rendering.
async function waitOutSpamTimer(page: Page) {
  await page.waitForTimeout(3_100);
}

test("happy path", async ({ page }) => {
  await page.goto("/demo");
  await fill(page);
  await waitOutSpamTimer(page);
  await page.getByRole("button", { name: "Request demo" }).click();
  await expect(page.getByRole("status")).toContainText("Thanks");
});

test("?plan= pre-selects the plan; unknown values are ignored", async ({ page }) => {
  await page.goto("/demo?plan=enterprise");
  await expect(page.getByLabel("Plan")).toHaveValue("enterprise");
  await page.goto("/demo?plan=free");
  await expect(page.getByLabel("Plan")).toHaveValue("");
});

test("invalid fields show inline errors, focus the first, and keep values", async ({ page }) => {
  await page.goto("/demo");
  await page.getByLabel("Name").fill("Ada");
  await page.getByLabel("Work email").fill("nope");
  await waitOutSpamTimer(page);
  await page.getByRole("button", { name: "Request demo" }).click();
  await expect(page.getByText("Enter a valid email address.")).toBeVisible();
  await expect(page.getByText("Enter your company.")).toBeVisible();
  await expect(page.getByLabel("Work email")).toBeFocused();
  await expect(page.getByLabel("Name")).toHaveValue("Ada");
  await expect(page.getByLabel("Work email")).toHaveAttribute("aria-invalid", "true");
});

test("webmail shows a non-blocking hint", async ({ page }) => {
  await page.goto("/demo");
  await page.getByLabel("Work email").fill("ada@gmail.com");
  await page.getByLabel("Work email").blur();
  await expect(page.getByText(/work address/)).toBeVisible();
});

test("send failure keeps values and offers a mailto fallback", async ({ page }) => {
  await page.goto("/demo");
  await fill(page, "__fail__");
  await waitOutSpamTimer(page);
  await page.getByRole("button", { name: "Request demo" }).click();
  const alert = page.getByRole("alert");
  await expect(alert).toContainText("Couldn't send");
  await expect(alert.getByRole("link", { name: "sales@example.test" })).toHaveAttribute("href", "mailto:sales@example.test");
  await expect(page.getByLabel("Name")).toHaveValue("Ada Lovelace");
});

test("honeypot is hidden from people and assistive tech", async ({ page }) => {
  await page.goto("/demo");
  const honeypot = page.locator('input[name="website"]');
  await expect(honeypot).toHaveAttribute("tabindex", "-1");
  await expect(page.locator('div[aria-hidden="true"]').filter({ has: honeypot })).toHaveCount(1);
  await expect(page.getByRole("textbox", { name: "Website" })).toHaveCount(0);
});

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false });
  test("the form still submits", async ({ page }) => {
    await page.goto("/demo?plan=team");
    await fill(page);
    await waitOutSpamTimer(page);
    await page.getByRole("button", { name: "Request demo" }).click();
    await expect(page.getByRole("status")).toContainText("Thanks");
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && pnpm test:e2e e2e/demo.spec.ts`
Expected: FAIL — `/demo` is 404.

- [ ] **Step 3: Server action** — `web/app/demo/actions.ts`:

```ts
"use server";

import { senderFromEnv } from "@/lib/demo/sender";
import { submitDemo, type DemoState } from "@/lib/demo/submit";

export async function submitDemoAction(_prev: DemoState, form: FormData): Promise<DemoState> {
  return submitDemo(form, {
    send: senderFromEnv(),
    inbox: process.env.DEMO_INBOX || null,
    from: process.env.DEMO_FROM || (process.env.DEMO_SENDER === "fake" ? "site@example.test" : null),
    now: Date.now,
    log: (msg, detail) => console.error(msg, detail),
  });
}
```

- [ ] **Step 4: Page** — `web/app/demo/page.tsx`. It reads `searchParams`, so Next renders it per request; `startedAt` is stamped here so the spam timer works without JavaScript.

```tsx
import { DemoForm } from "@/components/demo/demo-form";
import { parsePlan } from "@/lib/demo/schema";

export default async function DemoPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const { plan } = await searchParams;
  return (
    <div className="mx-auto max-w-2xl px-4 py-16 sm:px-6">
      <h1 className="text-4xl font-semibold tracking-tight">Request a demo</h1>
      <p className="mt-4 text-muted-foreground">
        Tell us which agents your developers use. We will reply by email.
      </p>
      <DemoForm plan={parsePlan(plan)} startedAt={Date.now()} />
    </div>
  );
}
```

- [ ] **Step 5: Form** — `web/components/demo/demo-form.tsx`:

```tsx
"use client";

import { useActionState, useEffect, useRef, useState } from "react";
import { submitDemoAction } from "@/app/demo/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { pricing, type PlanId } from "@/content/pricing";
import { agentOptions, deploymentOptions, isWebmail, sizeOptions } from "@/lib/demo/schema";
import type { DemoField, DemoState } from "@/lib/demo/submit";

const FIELD_ORDER: DemoField[] = ["name", "email", "company", "size", "agents", "deployment", "plan", "message"];
const nativeControl =
  "h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm shadow-xs focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 aria-invalid:border-destructive";

export function DemoForm({ plan, startedAt }: { plan: PlanId | undefined; startedAt: number }) {
  const [state, action, pending] = useActionState<DemoState, FormData>(submitDemoAction, { status: "idle" });
  const formRef = useRef<HTMLFormElement>(null);
  const [webmail, setWebmail] = useState(false);

  const errors = state.status === "invalid" ? state.fieldErrors : {};
  const values = state.status === "invalid" || state.status === "failed" ? state.values : {};
  const str = (k: string, fallback = "") => (typeof values[k] === "string" ? (values[k] as string) : fallback);
  const agents = Array.isArray(values.agents) ? values.agents : [];

  useEffect(() => {
    if (state.status !== "invalid") return;
    const first = FIELD_ORDER.find((f) => errors[f]);
    if (!first) return;
    const el = formRef.current?.querySelector<HTMLElement>(`[name="${first}"]`);
    el?.focus();
  }, [state, errors]);

  if (state.status === "ok") {
    return (
      <p role="status" className="mt-10 rounded-lg border border-border bg-card p-6">
        Thanks — we have your request and will reply by email.
      </p>
    );
  }

  const describedBy = (f: DemoField) => (errors[f] ? `${f}-error` : undefined);
  const ErrorText = ({ field }: { field: DemoField }) =>
    errors[field] ? (
      <p id={`${field}-error`} className="text-sm text-destructive">{errors[field]}</p>
    ) : null;

  // key forces a remount after each server response so defaultValue picks up returned values.
  return (
    <form ref={formRef} action={action} noValidate key={JSON.stringify(values)} className="mt-10 flex flex-col gap-6">
      {state.status === "failed" ? (
        <div role="alert" className="rounded-lg border border-destructive/50 p-4 text-sm">
          Couldn&apos;t send your request.{" "}
          {state.inbox ? (
            <>Email us at <a className="underline" href={`mailto:${state.inbox}`}>{state.inbox}</a>.</>
          ) : (
            "Please try again later."
          )}
        </div>
      ) : null}

      <input type="hidden" name="startedAt" value={startedAt} />
      <div aria-hidden="true" className="absolute -left-[9999px] h-px w-px overflow-hidden">
        <label htmlFor="website">Website</label>
        <input id="website" name="website" type="text" tabIndex={-1} autoComplete="off" />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="name">Name</Label>
        <Input id="name" name="name" autoComplete="name" defaultValue={str("name")} aria-invalid={!!errors.name} aria-describedby={describedBy("name")} />
        <ErrorText field="name" />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="email">Work email</Label>
        <Input
          id="email"
          name="email"
          type="email"
          autoComplete="email"
          defaultValue={str("email")}
          aria-invalid={!!errors.email}
          aria-describedby={[describedBy("email"), webmail ? "email-hint" : undefined].filter(Boolean).join(" ") || undefined}
          onBlur={(e) => setWebmail(isWebmail(e.currentTarget.value))}
        />
        {webmail ? (
          <p id="email-hint" className="text-sm text-muted-foreground">
            A work address helps us reply faster, but this one works too.
          </p>
        ) : null}
        <ErrorText field="email" />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="company">Company</Label>
        <Input id="company" name="company" autoComplete="organization" defaultValue={str("company")} aria-invalid={!!errors.company} aria-describedby={describedBy("company")} />
        <ErrorText field="company" />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="size">Company size</Label>
        <select id="size" name="size" defaultValue={str("size")} className={nativeControl} aria-invalid={!!errors.size} aria-describedby={describedBy("size")}>
          <option value="" disabled>Choose…</option>
          {sizeOptions.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
        </select>
        <ErrorText field="size" />
      </div>

      <fieldset className="flex flex-col gap-2" aria-describedby={describedBy("agents")}>
        <legend className="mb-2 text-sm font-medium">Agents your developers use</legend>
        {agentOptions.map((o) => (
          <label key={o.value} className="flex items-center gap-2 text-sm">
            <input type="checkbox" name="agents" value={o.value} defaultChecked={agents.includes(o.value)} className="size-4 accent-primary" />
            {o.label}
          </label>
        ))}
        <ErrorText field="agents" />
      </fieldset>

      <fieldset className="flex flex-col gap-2" aria-describedby={describedBy("deployment")}>
        <legend className="mb-2 text-sm font-medium">Deployment</legend>
        {deploymentOptions.map((o) => (
          <label key={o.value} className="flex items-center gap-2 text-sm">
            <input type="radio" name="deployment" value={o.value} defaultChecked={str("deployment") === o.value} className="size-4 accent-primary" />
            {o.label}
          </label>
        ))}
        <ErrorText field="deployment" />
      </fieldset>

      <div className="flex flex-col gap-2">
        <Label htmlFor="plan">Plan</Label>
        <select id="plan" name="plan" defaultValue={str("plan", plan ?? "")} className={nativeControl}>
          <option value="">Not sure yet</option>
          {pricing.tiers.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="message">Anything we should know? (optional)</Label>
        <Textarea id="message" name="message" rows={4} maxLength={2000} defaultValue={str("message")} aria-invalid={!!errors.message} aria-describedby={describedBy("message")} />
        <ErrorText field="message" />
      </div>

      <Button type="submit" disabled={pending} className="self-start">
        {pending ? "Sending…" : "Request demo"}
      </Button>
    </form>
  );
}
```

Notes for the implementer:
- Without JavaScript, the browser posts to the server action and Next re-renders the page with the returned `DemoState`; this is React's progressive enhancement for `useActionState` and is why `values` round-trip through the state.
- The "Company" label must not match "Company size" in `getByLabel("Company", { exact: true })` — keep the label text exactly `Company`.
- `ErrorText` defined inside the component is fine for this size; if lint (react/no-unstable-nested-components or the React compiler rules) objects, hoist it to module scope taking `errors` as a prop.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd web && pnpm lint && pnpm typecheck && pnpm test && pnpm test:e2e`
Expected: PASS, including the no-JS test.

- [ ] **Step 7: Commit**

```bash
git add web
git commit -m "feat(web): demo request page with progressive-enhancement form"
```

---

### Task 7: SEO — metadata, sitemap, robots, OG images, JSON-LD, llms.txt

**Files:**
- Create: `web/lib/seo.ts`, `web/lib/seo.test.ts`, `web/lib/og.tsx`, `web/components/json-ld.tsx`, `web/app/sitemap.ts`, `web/app/robots.ts`, `web/app/llms.txt/route.ts`, `web/app/opengraph-image.tsx`, `web/app/product/opengraph-image.tsx`, `web/app/pricing/opengraph-image.tsx`, `web/app/demo/opengraph-image.tsx`, `web/e2e/seo.spec.ts`
- Modify: `web/app/layout.tsx`, `web/app/page.tsx`, `web/app/product/page.tsx`, `web/app/pricing/page.tsx`, `web/app/demo/page.tsx`, `web/app/not-found.tsx`

**Interfaces:**
- Consumes: `siteUrl`, `isProductionDeploy`, `Env` from `@/lib/env`; `site`, `pricing` content.
- Produces: `routes` (the public route table), `pageMetadata(path: RoutePath, env?: Env): Metadata`, `robotsFor(env?: Env): MetadataRoute.Robots`, `sitemapFor(env?: Env): MetadataRoute.Sitemap`, `organizationJsonLd(env?)`, `softwareJsonLd(env?)`, `faqJsonLd()`, `llmsTxt(env?): string`, `<JsonLd data={object} />`, `renderOg(title: string, subtitle: string): Promise<ImageResponse>`.

Read `.claude/skills/seo-mastery` and the metadata sections of `.claude/skills/next-best-practices` first.

- [ ] **Step 1: Write the failing unit test** — `web/lib/seo.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import {
  faqJsonLd,
  llmsTxt,
  organizationJsonLd,
  pageMetadata,
  robotsFor,
  routes,
  sitemapFor,
  softwareJsonLd,
} from "./seo";
import { pricing } from "@/content/pricing";

const prod = { VERCEL_ENV: "production", NEXT_PUBLIC_SITE_URL: "https://warden.example" };
const preview = { VERCEL_ENV: "preview", NEXT_PUBLIC_SITE_URL: "https://preview.example" };

describe("routes", () => {
  it("covers every public page once", () => {
    expect(routes.map((r) => r.path)).toEqual(["/", "/product", "/pricing", "/demo"]);
  });
  it("has unique titles and descriptions", () => {
    expect(new Set(routes.map((r) => r.title)).size).toBe(routes.length);
    expect(new Set(routes.map((r) => r.description)).size).toBe(routes.length);
  });
});

describe("pageMetadata", () => {
  it("sets an absolute canonical from the site URL", () => {
    expect(pageMetadata("/pricing", prod).alternates?.canonical).toBe("https://warden.example/pricing");
    expect(pageMetadata("/", prod).alternates?.canonical).toBe("https://warden.example/");
  });
  it("indexes production and noindexes previews", () => {
    expect(pageMetadata("/", prod).robots).toEqual({ index: true, follow: true });
    expect(pageMetadata("/", preview).robots).toEqual({ index: false, follow: false });
    expect(pageMetadata("/", {}).robots).toEqual({ index: false, follow: false });
  });
});

describe("robotsFor", () => {
  it("allows all and names the sitemap in production", () => {
    expect(robotsFor(prod)).toEqual({
      rules: [{ userAgent: "*", allow: "/" }],
      sitemap: "https://warden.example/sitemap.xml",
    });
  });
  it("disallows all outside production", () => {
    expect(robotsFor(preview)).toEqual({ rules: [{ userAgent: "*", disallow: "/" }] });
  });
});

describe("sitemapFor", () => {
  it("lists every route with absolute URLs", () => {
    expect(sitemapFor(prod).map((e) => e.url)).toEqual([
      "https://warden.example/",
      "https://warden.example/product",
      "https://warden.example/pricing",
      "https://warden.example/demo",
    ]);
  });
});

describe("JSON-LD", () => {
  it("organization and software reference the site", () => {
    expect(organizationJsonLd(prod)).toMatchObject({ "@type": "Organization", url: "https://warden.example/" });
    expect(softwareJsonLd(prod)).toMatchObject({ "@type": "SoftwareApplication", applicationCategory: "DeveloperApplication" });
    expect(JSON.stringify(softwareJsonLd(prod))).not.toMatch(/"price"|aggregateRating|review/);
  });
  it("FAQPage mirrors the visible FAQ", () => {
    const ld = faqJsonLd();
    expect(ld["@type"]).toBe("FAQPage");
    expect(ld.mainEntity.map((q) => q.name)).toEqual(pricing.faq.map((f) => f.q));
  });
});

describe("llmsTxt", () => {
  it("links every route and the README", () => {
    const text = llmsTxt(prod);
    expect(text.startsWith("# Agentic Warden\n")).toBe(true);
    for (const url of sitemapFor(prod).map((e) => e.url)) expect(text).toContain(url);
    expect(text).toContain("https://github.com/alchristleo/agentic-warden#readme");
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && pnpm test lib/seo.test.ts`
Expected: FAIL — cannot resolve `./seo`.

- [ ] **Step 3: Implement** `web/lib/seo.ts`:

```ts
import type { Metadata, MetadataRoute } from "next";
import { pricing } from "@/content/pricing";
import { site } from "@/content/site";
import { isProductionDeploy, siteUrl, type Env } from "@/lib/env";

export const routes = [
  {
    path: "/",
    title: "Agentic Warden — one policy for every coding agent",
    description: site.description,
  },
  {
    path: "/product",
    title: "Product — Agentic Warden",
    description:
      "Group and repository targeting, signed bundles, SCIM provisioning and scheduled sync for Claude Code, Codex and Gemini CLI.",
  },
  {
    path: "/pricing",
    title: "Pricing — Agentic Warden",
    description: "Team, Enterprise and Self-hosted plans for governing AI coding agents. Contact us for a quote.",
  },
  {
    path: "/demo",
    title: "Request a demo — Agentic Warden",
    description: "See Agentic Warden applied to your own agent policy. Tell us which coding agents your developers use.",
  },
] as const;

export type RoutePath = (typeof routes)[number]["path"];

const abs = (path: string, env?: Env) => `${siteUrl(env)}${path}`;

export function pageMetadata(path: RoutePath, env: Env = process.env): Metadata {
  const route = routes.find((r) => r.path === path)!;
  const index = isProductionDeploy(env);
  return {
    title: { absolute: route.title },
    description: route.description,
    alternates: { canonical: abs(path, env) },
    robots: { index, follow: index },
    openGraph: {
      type: "website",
      siteName: site.name,
      title: route.title,
      description: route.description,
      url: abs(path, env),
    },
    twitter: { card: "summary_large_image", title: route.title, description: route.description },
  };
}

export function robotsFor(env: Env = process.env): MetadataRoute.Robots {
  if (!isProductionDeploy(env)) return { rules: [{ userAgent: "*", disallow: "/" }] };
  return { rules: [{ userAgent: "*", allow: "/" }], sitemap: abs("/sitemap.xml", env) };
}

export function sitemapFor(env: Env = process.env): MetadataRoute.Sitemap {
  return routes.map((r) => ({ url: abs(r.path, env) }));
}

export function organizationJsonLd(env: Env = process.env) {
  return {
    "@context": "https://schema.org",
    "@type": "Organization",
    name: site.name,
    url: abs("/", env),
    sameAs: [site.githubUrl],
  };
}

export function softwareJsonLd(env: Env = process.env) {
  return {
    "@context": "https://schema.org",
    "@type": "SoftwareApplication",
    name: site.name,
    description: site.description,
    url: abs("/", env),
    applicationCategory: "DeveloperApplication",
    operatingSystem: "Linux, macOS, Windows",
    license: site.licenseUrl,
  };
}

export function faqJsonLd() {
  return {
    "@context": "https://schema.org",
    "@type": "FAQPage",
    mainEntity: pricing.faq.map((f) => ({
      "@type": "Question",
      name: f.q,
      acceptedAnswer: { "@type": "Answer", text: f.a },
    })),
  };
}

export function llmsTxt(env: Env = process.env): string {
  const lines = [
    `# ${site.name}`,
    "",
    `> ${site.description}`,
    "",
    "## Pages",
    "",
    ...routes.map((r) => `- [${r.title}](${abs(r.path, env)}): ${r.description}`),
    "",
    "## Docs",
    "",
    `- [README](${site.docsUrl}): architecture, installation and policy format`,
    "",
  ];
  return lines.join("\n");
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `cd web && pnpm test lib/seo.test.ts`
Expected: PASS.

- [ ] **Step 5: Wire into Next.**

`web/components/json-ld.tsx`:

```tsx
// Escapes "<" so a string in the data can never close the script element.
export function JsonLd({ data }: { data: object }) {
  return (
    <script
      type="application/ld+json"
      dangerouslySetInnerHTML={{ __html: JSON.stringify(data).replace(/</g, "\\u003c") }}
    />
  );
}
```

`web/app/sitemap.ts`:

```ts
import type { MetadataRoute } from "next";
import { sitemapFor } from "@/lib/seo";

export default function sitemap(): MetadataRoute.Sitemap {
  return sitemapFor();
}
```

`web/app/robots.ts`:

```ts
import type { MetadataRoute } from "next";
import { robotsFor } from "@/lib/seo";

export default function robots(): MetadataRoute.Robots {
  return robotsFor();
}
```

`web/app/llms.txt/route.ts`:

```ts
import { llmsTxt } from "@/lib/seo";

export const dynamic = "force-static";

export function GET() {
  return new Response(llmsTxt(), { headers: { "content-type": "text/plain; charset=utf-8" } });
}
```

In `web/app/layout.tsx`: replace the `metadata` export with

```tsx
export const metadata: Metadata = {
  metadataBase: new URL(siteUrl()),
  ...pageMetadata("/"),
};
```

(import `siteUrl` from `@/lib/env` and `pageMetadata`, `organizationJsonLd`, `softwareJsonLd` from `@/lib/seo`), and render `<JsonLd data={organizationJsonLd()} />` and `<JsonLd data={softwareJsonLd()} />` as the first children of `<body>`.

In each page add a metadata export:
- `web/app/page.tsx`: `export const metadata = pageMetadata("/");`
- `web/app/product/page.tsx`: `export const metadata = pageMetadata("/product");`
- `web/app/pricing/page.tsx`: `export const metadata = pageMetadata("/pricing");` and render `<JsonLd data={faqJsonLd()} />` inside the page's root element.
- `web/app/demo/page.tsx`: `export const metadata = pageMetadata("/demo");`
- `web/app/not-found.tsx`: `export const metadata = { title: "Page not found — Agentic Warden", robots: { index: false } };`

- [ ] **Step 6: OG images.** `web/lib/og.tsx`:

```tsx
import { ImageResponse } from "next/og";
import { site } from "@/content/site";

export const ogSize = { width: 1200, height: 630 };

export async function renderOg(title: string, subtitle: string): Promise<ImageResponse> {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          padding: 72,
          background: "#0a0a0a",
          color: "#fafafa",
        }}
      >
        <div style={{ fontSize: 28, opacity: 0.7, fontFamily: "monospace" }}>{site.name}</div>
        <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
          <div style={{ fontSize: 72, fontWeight: 600, lineHeight: 1.1 }}>{title}</div>
          <div style={{ fontSize: 32, opacity: 0.7 }}>{subtitle}</div>
        </div>
      </div>
    ),
    { ...ogSize },
  );
}
```

Brand font: look under `node_modules/geist/dist/fonts/` for a `.ttf` or `.otf` of Geist SemiBold. If one exists, read it with `readFile` (from `node:fs/promises`, path via `path.join(process.cwd(), "node_modules/geist/dist/fonts/...")`) and pass `fonts: [{ name: "Geist", data, weight: 600 }]` with `fontFamily: "Geist"` on the root. If only `.woff2` exists, keep the default font and say so in the report (planning ruling).

Each route file (all four identical in shape; titles differ):

`web/app/opengraph-image.tsx`:

```tsx
import { ogSize, renderOg } from "@/lib/og";

export const alt = "Agentic Warden — one policy for every coding agent";
export const size = ogSize;
export const contentType = "image/png";

export default function Image() {
  return renderOg("One policy for every coding agent", "Claude Code · Codex · Gemini CLI");
}
```

`web/app/product/opengraph-image.tsx`:

```tsx
import { ogSize, renderOg } from "@/lib/og";

export const alt = "Agentic Warden product overview";
export const size = ogSize;
export const contentType = "image/png";

export default function Image() {
  return renderOg("Govern coding agents like the rest of your fleet", "Targeting · Signed bundles · SCIM · Sync");
}
```

`web/app/pricing/opengraph-image.tsx`:

```tsx
import { ogSize, renderOg } from "@/lib/og";

export const alt = "Agentic Warden pricing";
export const size = ogSize;
export const contentType = "image/png";

export default function Image() {
  return renderOg("Pricing", "Team · Enterprise · Self-hosted");
}
```

`web/app/demo/opengraph-image.tsx`:

```tsx
import { ogSize, renderOg } from "@/lib/og";

export const alt = "Request an Agentic Warden demo";
export const size = ogSize;
export const contentType = "image/png";

export default function Image() {
  return renderOg("Request a demo", "See it against your own policy");
}
```

- [ ] **Step 7: Write the e2e SEO test** — `web/e2e/seo.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

const paths = ["/", "/product", "/pricing", "/demo"];

test("each route has a unique title, description and canonical", async ({ page }) => {
  const seen = { title: new Set<string>(), description: new Set<string>() };
  for (const path of paths) {
    await page.goto(path);
    const title = await page.title();
    const description = await page.locator('meta[name="description"]').getAttribute("content");
    const canonical = await page.locator('link[rel="canonical"]').getAttribute("href");
    expect(title).toBeTruthy();
    expect(description).toBeTruthy();
    expect(canonical).toBe(`http://localhost:3000${path}`);
    seen.title.add(title);
    seen.description.add(description!);
  }
  expect(seen.title.size).toBe(paths.length);
  expect(seen.description.size).toBe(paths.length);
});

test("JSON-LD blocks parse, and pricing carries FAQPage", async ({ page }) => {
  for (const path of paths) {
    await page.goto(path);
    const blocks = await page.locator('script[type="application/ld+json"]').allTextContents();
    expect(blocks.length).toBeGreaterThanOrEqual(2);
    const types = blocks.map((b) => JSON.parse(b)["@type"]);
    expect(types).toEqual(expect.arrayContaining(["Organization", "SoftwareApplication"]));
    if (path === "/pricing") expect(types).toContain("FAQPage");
  }
});

test("sitemap lists every route", async ({ request }) => {
  const body = await (await request.get("/sitemap.xml")).text();
  for (const path of paths) expect(body).toContain(`<loc>http://localhost:3000${path}</loc>`);
});

test("local (non-production) build is not indexable", async ({ page, request }) => {
  const robots = await (await request.get("/robots.txt")).text();
  expect(robots).toMatch(/Disallow: \//);
  await page.goto("/");
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute("content", /noindex/);
});

test("OG images render", async ({ request }) => {
  for (const path of ["/opengraph-image", "/product/opengraph-image", "/pricing/opengraph-image", "/demo/opengraph-image"]) {
    const res = await request.get(path);
    expect(res.status()).toBe(200);
    expect(res.headers()["content-type"]).toContain("image/png");
  }
});

test("llms.txt is served", async ({ request }) => {
  const res = await request.get("/llms.txt");
  expect(res.status()).toBe(200);
  expect(await res.text()).toContain("# Agentic Warden");
});
```

If Next serves OG images at a hashed path (e.g. `/opengraph-image-<hash>`), read the URL from each page's `meta[property="og:image"]` instead of hard-coding it, and keep the 200 + `image/png` assertions.

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd web && pnpm lint && pnpm typecheck && pnpm test && pnpm test:e2e`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add web
git commit -m "feat(web): metadata, sitemap, robots, OG images, JSON-LD and llms.txt"
```

---

### Task 8: Security headers, accessibility, Lighthouse CI, workflow, README

**Files:**
- Create: `web/lib/headers.ts`, `web/lib/headers.test.ts`, `web/e2e/a11y.spec.ts`, `web/e2e/headers.spec.ts`, `web/lighthouserc.json`, `.github/workflows/web.yml`
- Modify: `web/next.config.ts`, root `README.md`

**Interfaces:**
- Consumes: everything above.
- Produces: `securityHeaders(): { key: string; value: string }[]`.

- [ ] **Step 1: Write the failing header unit test** — `web/lib/headers.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { securityHeaders } from "./headers";

const get = (key: string) => securityHeaders().find((h) => h.key === key)?.value;

describe("securityHeaders", () => {
  it("sets a CSP without unsafe-eval", () => {
    const csp = get("Content-Security-Policy")!;
    for (const d of [
      "default-src 'self'",
      "script-src 'self' 'unsafe-inline'",
      "object-src 'none'",
      "base-uri 'self'",
      "form-action 'self'",
      "frame-ancestors 'none'",
    ]) {
      expect(csp).toContain(d);
    }
    expect(csp).not.toContain("unsafe-eval");
  });
  it("sets HSTS, referrer and permissions policies", () => {
    expect(get("Strict-Transport-Security")).toBe("max-age=63072000; includeSubDomains; preload");
    expect(get("Referrer-Policy")).toBe("strict-origin-when-cross-origin");
    expect(get("Permissions-Policy")).toBe("camera=(), microphone=(), geolocation=()");
    expect(get("X-Content-Type-Options")).toBe("nosniff");
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && pnpm test lib/headers.test.ts`
Expected: FAIL — cannot resolve `./headers`.

- [ ] **Step 3: Implement** `web/lib/headers.ts`:

```ts
// Nonces would force every route to render per request, so the CSP allows
// inline scripts instead; the site renders no user-supplied HTML.
const csp = [
  "default-src 'self'",
  "script-src 'self' 'unsafe-inline'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data: blob:",
  "font-src 'self'",
  "connect-src 'self'",
  "object-src 'none'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
  "upgrade-insecure-requests",
].join("; ");

export function securityHeaders(): { key: string; value: string }[] {
  return [
    { key: "Content-Security-Policy", value: csp },
    { key: "Strict-Transport-Security", value: "max-age=63072000; includeSubDomains; preload" },
    { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
    { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
    { key: "X-Content-Type-Options", value: "nosniff" },
  ];
}
```

Wire into `web/next.config.ts` (production only — `next dev` needs `eval` for fast refresh):

```ts
import type { NextConfig } from "next";
import { assertBuildEnv } from "./lib/env";
import { securityHeaders } from "./lib/headers";

assertBuildEnv();

const nextConfig: NextConfig = {
  poweredByHeader: false,
  async headers() {
    if (process.env.NODE_ENV !== "production") return [];
    return [{ source: "/:path*", headers: securityHeaders() }];
  },
};

export default nextConfig;
```

Keep any other option create-next-app generated in `nextConfig`. `upgrade-insecure-requests` is harmless on `http://localhost` in Chromium (localhost is exempt); if it breaks the local Playwright run, drop that directive and note it.

- [ ] **Step 4: Run it to verify it passes**

Run: `cd web && pnpm test lib/headers.test.ts`
Expected: PASS.

- [ ] **Step 5: e2e for headers and accessibility.** `web/e2e/headers.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

test("production server sends security headers", async ({ request }) => {
  const res = await request.get("/");
  const h = res.headers();
  expect(h["content-security-policy"]).toContain("frame-ancestors 'none'");
  expect(h["strict-transport-security"]).toContain("max-age=");
  expect(h["x-powered-by"]).toBeUndefined();
});

test("pages run without CSP violations", async ({ page }) => {
  const violations: string[] = [];
  page.on("console", (msg) => {
    if (/Content Security Policy/i.test(msg.text())) violations.push(msg.text());
  });
  for (const path of ["/", "/product", "/pricing", "/demo"]) {
    await page.goto(path);
    await page.waitForLoadState("networkidle");
  }
  expect(violations).toEqual([]);
});
```

`web/e2e/a11y.spec.ts`:

```ts
import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

const paths = ["/", "/product", "/pricing", "/demo", "/no-such-page"];

for (const theme of ["dark", "light"] as const) {
  for (const path of paths) {
    test(`${path} has no axe violations in ${theme} mode`, async ({ page }) => {
      await page.addInitScript((t) => localStorage.setItem("theme", t), theme);
      await page.goto(path);
      await expect(page.locator("html")).toHaveClass(theme === "dark" ? /dark/ : /^(?!.*\bdark\b)/);
      const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze();
      expect(results.violations).toEqual([]);
    });
  }
}
```

Fix every violation in the components (contrast, labels, landmarks), not by disabling rules.

- [ ] **Step 6: Run the e2e suite**

Run: `cd web && pnpm test:e2e`
Expected: PASS. Fix failures in the site code.

- [ ] **Step 7: Lighthouse CI** — `web/lighthouserc.json`:

```json
{
  "ci": {
    "collect": {
      "startServerCommand": "pnpm start",
      "startServerReadyPattern": "Ready",
      "url": [
        "http://localhost:3000/",
        "http://localhost:3000/product",
        "http://localhost:3000/pricing",
        "http://localhost:3000/demo"
      ],
      "numberOfRuns": 1
    },
    "assert": {
      "assertions": {
        "categories:performance": ["error", { "minScore": 0.9 }],
        "categories:accessibility": ["error", { "minScore": 1 }],
        "categories:best-practices": ["error", { "minScore": 0.95 }],
        "categories:seo": ["error", { "minScore": 0.95 }]
      }
    },
    "upload": { "target": "filesystem", "outputDir": ".lighthouseci" }
  }
}
```

Lighthouse's default form factor is mobile. The SEO category penalises `noindex`, so run Lighthouse against a production-mode build: `VERCEL_ENV=production` with placeholder values for the required env and the real sender left unset — `assertBuildEnv` requires `RESEND_API_KEY`, so pass a dummy value (`re_dummy`); no form is submitted during Lighthouse.

Run locally (WSL has no system Chrome; point LHCI at Playwright's Chromium):

```bash
cd web
export VERCEL_ENV=production NEXT_PUBLIC_SITE_URL=http://localhost:3000 RESEND_API_KEY=re_dummy DEMO_INBOX=sales@example.test DEMO_FROM=site@example.test
pnpm build
CHROME_PATH="$(node -e "console.log(require('playwright-core').chromium.executablePath())")" pnpm lhci
```

(If `playwright-core` is not resolvable from `web/`, use `pnpm exec playwright` 's bundled path: `node -e "console.log(require('@playwright/test').chromium.executablePath())"`.)
Expected: all four assertions pass on all four URLs. If Performance lands under 0.9, find the cause in the Lighthouse report (client JS, fonts, image sizes) and fix it; never lower the threshold.

- [ ] **Step 8: Workflow** — `.github/workflows/web.yml`:

```yaml
name: web

on:
  push:
    branches: [main]
    paths: ["web/**", ".github/workflows/web.yml"]
  pull_request:
    paths: ["web/**", ".github/workflows/web.yml"]

permissions:
  contents: read

defaults:
  run:
    working-directory: web

jobs:
  test:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@v4
      - uses: pnpm/action-setup@v4
        with:
          package_json_file: web/package.json
      - uses: actions/setup-node@v4
        with:
          node-version: 20
          cache: pnpm
          cache-dependency-path: web/pnpm-lock.yaml
      - run: pnpm install --frozen-lockfile
      - run: pnpm lint
      - run: pnpm typecheck
      - run: pnpm test
      - run: pnpm exec playwright install --with-deps chromium
      - run: pnpm test:e2e
        env:
          CI: "true"
      - name: Lighthouse (production-mode build)
        env:
          VERCEL_ENV: production
          NEXT_PUBLIC_SITE_URL: http://localhost:3000
          RESEND_API_KEY: re_dummy
          DEMO_INBOX: sales@example.test
          DEMO_FROM: site@example.test
        run: |
          pnpm build
          pnpm lhci
```

`pnpm/action-setup` reads the pnpm version from `packageManager` in `web/package.json`; ensure that field exists (`"packageManager": "pnpm@10.34.5"` or whatever `pnpm -v` prints). The `CI` env makes Playwright start a fresh server instead of reusing one.

- [ ] **Step 9: README.** Append to the root `README.md`:

```markdown
## Marketing site

`web/` is the marketing site: a Next.js project with its own pnpm lockfile,
deployed to Vercel with `web/` as the project root. It does not share code
with the Go module.

    cd web
    pnpm install
    pnpm dev            # http://localhost:3000
    pnpm test           # unit tests
    pnpm test:e2e       # Playwright against a production build

Production deploys (`VERCEL_ENV=production`) need `NEXT_PUBLIC_SITE_URL`,
`RESEND_API_KEY`, `DEMO_INBOX` and `DEMO_FROM`; the build fails without
them. See `web/.env.example`.
```

- [ ] **Step 10: Full verification**

Run: `cd web && pnpm lint && pnpm typecheck && pnpm test && pnpm test:e2e`, then the Lighthouse commands from Step 7.
Expected: all green. Also run `go build ./... && go test ./...` from the repo root to confirm nothing outside `web/` changed behaviour.

- [ ] **Step 11: Commit**

```bash
git add web .github/workflows/web.yml README.md
git commit -m "feat(web): security headers, accessibility and Lighthouse gates in CI"
```
