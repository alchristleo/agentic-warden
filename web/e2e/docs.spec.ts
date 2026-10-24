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

test("code blocks have a copy button that copies the code", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("/docs");
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
