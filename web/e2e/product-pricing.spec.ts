import { expect, test } from "@playwright/test";

test("product lists shipped capabilities and badges the console", async ({ page }) => {
  await page.goto("/product");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  for (const name of [
    "Group and repository targeting",
    "Signed bundles",
    "SCIM provisioning",
    "Scheduled sync on every OS",
    "Fails safe by default",
    "aw doctor",
    "Hosted console",
  ]) {
    await expect(page.getByRole("heading", { level: 2, name })).toBeVisible();
  }
  const console = page.getByRole("region", { name: "Hosted console" });
  await expect(console.getByText("Coming soon")).toBeVisible();
});

test("pricing shows three tiers, all Contact us, with plan-specific CTAs", async ({ page }) => {
  await page.goto("/pricing");
  for (const [tier, plan] of [["Team", "team"], ["Enterprise", "enterprise"], ["Self-hosted", "self-hosted"]] as const) {
    const card = page.getByRole("article", { name: tier });
    await expect(card.getByText("Contact us")).toBeVisible();
    await expect(card.getByRole("link", { name: /Request demo|Talk to us/ })).toHaveAttribute("href", `/demo?plan=${plan}`);
  }
  await expect(page.getByRole("article", { name: "Team" }).getByText("Coming soon")).toBeVisible();
  await expect(page.getByText(/\$\s?\d/)).toHaveCount(0);
});

test("pricing FAQ answers are in the page", async ({ page }) => {
  await page.goto("/pricing");
  const faq = page.getByRole("region", { name: "Frequently asked questions" });
  await expect(faq.locator("details")).toHaveCount(6);
  await faq.getByText("What happens if the control plane is down?").click();
  await expect(faq.getByText(/last synced bundle/)).toBeVisible();
});

test("header navigation reaches both pages", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("banner").getByRole("link", { name: "Product" }).click();
  await expect(page).toHaveURL(/\/product$/);
  await page.getByRole("banner").getByRole("link", { name: "Pricing" }).click();
  await expect(page).toHaveURL(/\/pricing$/);
});
