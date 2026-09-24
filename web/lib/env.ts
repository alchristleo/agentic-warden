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
