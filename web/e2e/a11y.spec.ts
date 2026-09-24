import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

const paths = ["/", "/product", "/pricing", "/demo", "/no-such-page"];

for (const theme of ["dark", "light"] as const) {
  for (const path of paths) {
    test(`${path} has no axe violations in ${theme} mode`, async ({ page }) => {
      await page.addInitScript((t) => localStorage.setItem("theme", t), theme);
      await page.goto(path);
      await expect(page.locator("html")).toHaveClass(theme === "dark" ? /dark/ : /^(?!.*\bdark\b)/);
      const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze();
      expect(results.violations).toEqual([]);
    });
  }
}
