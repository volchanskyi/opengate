import { test, expect } from "./fixtures";
import AxeBuilder from "@axe-core/playwright";

// Allowlist of accessibility violation rule IDs that are pre-existing and
// intentionally waived. New violations (rules not in this list) fail the
// a11y gate. Mirror any waivers in e2e/a11y-baseline.json (the JSON is the
// human-facing reviewable form; this constant is the runtime source of
// truth — playwright runs without node FS in the browser-side bundle).
const WAIVED_RULES: ReadonlySet<string> = new Set([
  // Pre-existing color-contrast issues on form-helper text and link-on-text
  // affordances (e.g. the "Register" link inline in the login footer). Tracked
  // for a future design pass; the a11y gate inventories the rule set so a NEW
  // violation (a different rule ID) blocks merge.
  "color-contrast",
  "link-in-text-block",
  "link-in-text-block-style",
]);

// Filter out violations whose rule IDs appear in the allowlist above. Any
// other WCAG 2.1 A/AA violation fails the test and blocks merge.
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
    // Match device-list.spec.ts empty-state copy.
    await expect(
      authedPage.getByText(/no groups|no devices|create.*group/i),
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
