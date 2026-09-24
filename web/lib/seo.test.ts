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
