import { test, expect } from "./fixtures";
import AxeBuilder from "@axe-core/playwright";

// Rule IDs waived by the a11y gate; a violation of any other rule fails the test.
const WAIVED_RULES: ReadonlySet<string> = new Set([
  "color-contrast",
  "link-in-text-block",
  "link-in-text-block-style",
]);

function unwaivedViolations(violations: Array<{ id: string }>) {
  return violations.filter((v) => !WAIVED_RULES.has(v.id));
}

test.describe("Accessibility (WCAG 2.1 A/AA)", () => {
  test("login page has no axe violations", async ({ page }) => {
    await page.goto("/login");
    await expect(page.getByRole("button", { name: "Login" })).toBeVisible();

    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    expect(unwaivedViolations(results.violations)).toEqual([]);
  });

  test("register page has no axe violations", async ({ page }) => {
    await page.goto("/register");
    await expect(page.getByRole("button", { name: "Register" })).toBeVisible();

    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    expect(unwaivedViolations(results.violations)).toEqual([]);
  });

  test("device list (empty) has no axe violations", async ({ authedPage }) => {
    await authedPage.goto("/devices");
    await expect(
      authedPage.getByText(/no sites|no devices|create.*site/i),
    ).toBeVisible();

    const results = await new AxeBuilder({ page: authedPage })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    expect(unwaivedViolations(results.violations)).toEqual([]);
  });

  test("admin user management has no axe violations", async ({ adminPage }) => {
    await adminPage.goto("/settings/users");
    await expect(
      adminPage.getByRole("heading", { name: /user management/i }),
    ).toBeVisible();

    const results = await new AxeBuilder({ page: adminPage })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    expect(unwaivedViolations(results.violations)).toEqual([]);
  });

  test("admin audit log has no axe violations", async ({ adminPage }) => {
    await adminPage.goto("/settings/audit");
    await expect(adminPage.getByRole("heading", { name: /audit/i })).toBeVisible();

    const results = await new AxeBuilder({ page: adminPage })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    expect(unwaivedViolations(results.violations)).toEqual([]);
  });
});
