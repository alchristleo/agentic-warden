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
