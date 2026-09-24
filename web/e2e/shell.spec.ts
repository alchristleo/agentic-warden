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
