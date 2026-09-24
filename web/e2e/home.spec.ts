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
