import { expect, test } from "@playwright/test";
import { docSlug, loadDocs, navSections } from "../lib/docs";

const docs = loadDocs();
const url = (slug: string) => ["/docs", ...docSlug(slug)].join("/");

test("every docs page renders its title as the h1", async ({ page }) => {
  expect(docs.length).toBeGreaterThan(0);
  for (const d of docs) {
    const res = await page.goto(url(d.slug));
    expect(res?.status(), d.slug).toBe(200);
    await expect(page.getByRole("heading", { level: 1 }), d.slug).toHaveText(d.title);
  }
});

test("nav shows the sections that have pages in the fixed order", async ({ page }) => {
  await page.goto("/docs");
  const nav = page.getByRole("navigation", { name: "Docs" });
  const expected: string[] = navSections(docs).map((s) => s.label);
  await expect(nav.getByRole("heading", { level: 2 })).toHaveText(expected);
  const order: string[] = ["Get started", "Security review", "Guides", "Reference"];
  expect(expected).toEqual(order.filter((o) => expected.includes(o)));
});

test("every internal /docs link returns 200", async ({ page, request }) => {
  const hrefs = new Set<string>();
  for (const d of docs) {
    await page.goto(url(d.slug));
    for (const h of await page.locator("a[href^='/docs']").evaluateAll((as) => as.map((a) => a.getAttribute("href")!))) {
      hrefs.add(h.split("#")[0]);
    }
  }
  for (const h of hrefs) expect((await request.get(h)).status(), h).toBe(200);
});

test("sitemap lists every docs page", async ({ request }) => {
  const body = await (await request.get("/sitemap.xml")).text();
  for (const d of docs) expect(body).toContain(`<loc>http://localhost:3000${url(d.slug)}</loc>`);
});

test("code blocks have a copy button that copies the code", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/docs/quickstart");
  const block = page.locator("main .code-block").first();
  await expect(block).toBeVisible();
  const code = (await block.locator("pre").innerText()).trim();
  await block.getByRole("button", { name: "Copy" }).click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(code);
});

test("unknown docs page and unknown or traversing asset are 404", async ({ request }) => {
  expect((await request.get("/docs/no-such-page")).status()).toBe(404);
  expect((await request.get("/docs-assets/no-such.gif")).status()).toBe(404);
  expect((await request.get("/docs-assets/..%2Fsite%2Findex.md")).status()).toBe(404);
});

test("a docs asset is served", async ({ request }) => {
  const res = await request.get("/docs-assets/logo.svg");
  expect(res.status()).toBe(200);
  expect(res.headers()["content-type"]).toContain("image/svg+xml");
});

test("the current page is marked in the nav and the demo band shows once", async ({ page }) => {
  await page.goto("/docs/quickstart");
  const nav = page.getByRole("navigation", { name: "Docs" });
  await expect(nav.locator("[aria-current='page']")).toHaveCount(1);
  await expect(page.locator("main").getByText(/Bring your rules/)).toHaveCount(1);
  const band = page.locator("main aside").filter({ hasText: "Bring your rules." });
  await expect(band).toHaveCount(1);
  await expect(band.getByRole("link", { name: "Request a demo" })).toHaveAttribute("href", "/demo");
});

test.describe("mobile", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("no horizontal overflow, Docs link in header, nav collapses", async ({ page }) => {
    await page.goto("/docs/quickstart");
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
    await expect(page.getByRole("banner").getByRole("link", { name: "Docs" })).toBeVisible();
    const menu = page.getByText("Docs menu");
    await expect(menu).toBeVisible();
    const first = page.getByRole("navigation", { name: "Docs" }).getByRole("link").first();
    await expect(first).toBeHidden();
    await menu.click();
    await expect(first).toBeVisible();
  });

  test("table code stays on one line", async ({ page }) => {
    await page.goto("/docs/reference");
    const cells = page.locator(".docs-prose td code");
    expect(await cells.count()).toBeGreaterThanOrEqual(5);
    const rects = await page.$$eval(".docs-prose td code, .docs-prose th code", (els) =>
      els.map((e) => e.getClientRects().length),
    );
    expect(rects.every((n) => n === 1)).toBe(true);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  });

  test("threat-model tables fit the column on desktop", async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    await page.goto("/docs/threat-model");
    const fits = await page.$$eval(".docs-prose table", (els) =>
      els.map((e) => e.scrollWidth <= e.clientWidth + 1),
    );
    expect(fits.length).toBeGreaterThan(0);
    expect(fits.every(Boolean)).toBe(true);
  });
});
