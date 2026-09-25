import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

async function signIn(page: Page, email: string) {
  await page.goto("/console/");
  await page.getByRole("link", { name: "Sign in" }).click();
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Sign in" }).click();
}

test("admin signs in, applies a revision, revokes a machine, and sees both in audit", async ({ page }) => {
  await signIn(page, "admin@example.com");
  await expect(page.getByText("admin@example.com")).toBeVisible();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);

  await page.getByRole("link", { name: "Policy" }).click();
  await page.getByRole("button", { name: /new revision/i }).click();
  const editor = page.getByRole("textbox", { name: /policy yaml/i });
  await editor.click();
  await page.keyboard.press("ControlOrMeta+a");
  // CodeMirror auto-indents and auto-closes brackets while typing, which
  // corrupts YAML typed key by key with page.keyboard.type(); insertText
  // pastes the text as one edit instead, bypassing those input rules.
  await page.keyboard.insertText("version: v2\nrules:\n  - name: baseline\n");
  await page.getByRole("button", { name: /review changes/i }).click();
  await page.getByRole("dialog").getByRole("button", { name: /apply revision/i }).click();
  await expect(page.getByText(/applied v2/i)).toBeVisible();
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);

  await page.getByRole("link", { name: "Machines" }).click();
  await page.getByRole("button", { name: /revoke e2e-laptop/i }).click();
  // Scoped to the dialog: the dialog itself carries the same accessible
  // name (its title, via aria-labelledby) as the confirmation input's
  // <label>, so an unscoped getByLabel resolves to both and is ambiguous.
  const revokeDialog = page.getByRole("dialog");
  await revokeDialog.getByLabel(/type e2e-laptop/i).fill("e2e-laptop");
  await revokeDialog.getByRole("button", { name: /revoke machine/i }).click();
  await expect(page.getByText(/revoked e2e-laptop/i)).toBeVisible();

  await page.getByRole("link", { name: "Audit" }).click();
  const rows = page.getByRole("row");
  await expect(rows.filter({ hasText: "machine.revoke" }).filter({ hasText: "admin@example.com" })).toHaveCount(1);
  await expect(rows.filter({ hasText: "policy.revision.create" }).filter({ hasText: "admin@example.com" })).toHaveCount(1);
  expect((await new AxeBuilder({ page }).analyze()).violations).toEqual([]);
});

test("a non-admin is refused", async ({ page }) => {
  await signIn(page, "dev@example.com");
  await expect(page.getByText(/not a console admin/i)).toBeVisible();
});

test("sign out ends the session", async ({ page }) => {
  await signIn(page, "admin@example.com");
  await page.getByRole("button", { name: /sign out/i }).click();
  await expect(page.getByRole("link", { name: "Sign in" })).toBeVisible();
  await page.goto("/console/machines");
  await expect(page.getByRole("link", { name: "Sign in" })).toBeVisible();
});

test("every screen passes axe in dark mode", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await signIn(page, "admin@example.com");
  for (const name of ["Overview", "Policy", "Machines", "Enrollment", "Groups", "Audit"]) {
    await page.getByRole("link", { name }).click();
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    expect((await new AxeBuilder({ page }).analyze()).violations, name).toEqual([]);
  }
});
