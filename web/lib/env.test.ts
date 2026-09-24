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
