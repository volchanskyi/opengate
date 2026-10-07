import { test, expect } from "./fixtures";
import { stubEmptyFleet } from "./helpers/fleet-stub";

// Chromium-only baselines; `maxDiffPixelRatio: 0.01` tolerates minor font-rendering jitter.

const screenshotOptions = { maxDiffPixelRatio: 0.01 } as const;

test.describe("Visual regression (Chromium baselines)", () => {
  test.skip(
    ({ browserName }) => browserName !== "chromium",
    "Visual regression baselines are Chromium-only",
  );

  test("login page", async ({ page }) => {
    await page.goto("/login");
    await expect(page.getByRole("button", { name: "Login" })).toBeVisible();
    await expect(page).toHaveScreenshot("login.png", screenshotOptions);
  });

  test("register page", async ({ page }) => {
    await page.goto("/register");
    await expect(page.getByRole("button", { name: "Register" })).toBeVisible();
    await expect(page).toHaveScreenshot("register.png", screenshotOptions);
  });

  test("device list (empty)", async ({ authedPage }) => {
    await stubEmptyFleet(authedPage);
    await authedPage.goto("/devices");
    await expect(authedPage.getByText("No sites yet")).toBeVisible();
    await expect(authedPage).toHaveScreenshot("device-list-empty.png", screenshotOptions);
  });

  test("admin user management", async ({ adminPage }) => {
    await adminPage.goto("/settings/users");
    await expect(
      adminPage.getByRole("heading", { name: /user management/i }),
    ).toBeVisible();
    // Other specs seed an unpredictable number of users, so the tbody is hidden to keep its height out.
    await expect(adminPage.locator('[data-testid="user-email-cell"]').first()).toBeVisible();
    await adminPage.addStyleTag({ content: "table tbody { display: none !important; }" });
    await expect(adminPage).toHaveScreenshot("admin-users.png", screenshotOptions);
  });
});
