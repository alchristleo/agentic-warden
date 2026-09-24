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
    // Next's metadata resolver collapses the root canonical to the bare
    // origin (no trailing slash) — see resolveAbsoluteUrlWithPathname in
    // next/dist/lib/metadata/resolvers/resolve-url.js.
    expect(canonical).toBe(path === "/" ? "http://localhost:3000" : `http://localhost:3000${path}`);
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
