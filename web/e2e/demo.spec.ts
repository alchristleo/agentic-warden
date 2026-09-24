import { expect, test, type Page } from "@playwright/test";

async function fill(page: Page, company = "Acme") {
  await page.getByLabel("Name").fill("Ada Lovelace");
  await page.getByLabel("Work email").fill("ada@acme.com");
  await page.getByLabel("Company", { exact: true }).fill(company);
  await page.getByLabel("Company size").selectOption("51-500");
  await page.getByRole("checkbox", { name: "Claude Code" }).check();
  await page.getByRole("radio", { name: "Hosted by us" }).check();
}

// The server drops submissions made within 3 s of rendering.
async function waitOutSpamTimer(page: Page) {
  await page.waitForTimeout(3_100);
}

test("happy path", async ({ page }) => {
  await page.goto("/demo");
  await fill(page);
  await waitOutSpamTimer(page);
  await page.getByRole("button", { name: "Request demo" }).click();
  await expect(page.getByRole("status")).toContainText("Thanks");
});

test("?plan= pre-selects the plan; unknown values are ignored", async ({ page }) => {
  await page.goto("/demo?plan=enterprise");
  await expect(page.getByLabel("Plan")).toHaveValue("enterprise");
  await page.goto("/demo?plan=free");
  await expect(page.getByLabel("Plan")).toHaveValue("");
});

test("invalid fields show inline errors, focus the first, and keep values", async ({ page }) => {
  await page.goto("/demo");
  await page.getByLabel("Name").fill("Ada");
  await page.getByLabel("Work email").fill("nope");
  await waitOutSpamTimer(page);
  await page.getByRole("button", { name: "Request demo" }).click();
  await expect(page.getByText("Enter a valid email address.")).toBeVisible();
  await expect(page.getByText("Enter your company.")).toBeVisible();
  await expect(page.getByLabel("Work email")).toBeFocused();
  await expect(page.getByLabel("Name")).toHaveValue("Ada");
  await expect(page.getByLabel("Work email")).toHaveAttribute("aria-invalid", "true");
});

test("webmail shows a non-blocking hint", async ({ page }) => {
  await page.goto("/demo");
  await page.getByLabel("Work email").fill("ada@gmail.com");
  await page.getByLabel("Work email").blur();
  await expect(page.getByText(/work address/)).toBeVisible();
});

test("send failure keeps values and offers a mailto fallback", async ({ page }) => {
  await page.goto("/demo");
  await fill(page, "__fail__");
  await waitOutSpamTimer(page);
  await page.getByRole("button", { name: "Request demo" }).click();
  // Scoped past text: Next's client-side route announcer also renders a
  // permanent, empty `role="alert"` element (in a shadow root) on every
  // hydrated page, so an unscoped getByRole("alert") is ambiguous.
  const alert = page.getByRole("alert").filter({ hasText: "Couldn't send" });
  await expect(alert).toContainText("Couldn't send");
  await expect(alert.getByRole("link", { name: "sales@example.test" })).toHaveAttribute("href", "mailto:sales@example.test");
  await expect(page.getByLabel("Name")).toHaveValue("Ada Lovelace");
});

test("honeypot is hidden from people and assistive tech", async ({ page }) => {
  await page.goto("/demo");
  const honeypot = page.locator('input[name="website"]');
  await expect(honeypot).toHaveAttribute("tabindex", "-1");
  await expect(page.locator('div[aria-hidden="true"]').filter({ has: honeypot })).toHaveCount(1);
  await expect(page.getByRole("textbox", { name: "Website" })).toHaveCount(0);
});

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false });
  test("the form still submits", async ({ page }) => {
    await page.goto("/demo?plan=team");
    await fill(page);
    await waitOutSpamTimer(page);
    await page.getByRole("button", { name: "Request demo" }).click();
    await expect(page.getByRole("status")).toContainText("Thanks");
  });
});
