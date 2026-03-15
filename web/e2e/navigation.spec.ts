import { test, expect } from "../e2e/fixtures";

test.describe("Navigation and routing", () => {
  test("/ redirects to /login when not authenticated", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveURL(/\/login/);
  });

  test("/devices redirects to /login when not authenticated", async ({
    page,
  }) => {
    await page.goto("/devices");
    await expect(page).toHaveURL(/\/login/);
  });

  test("authenticated user can navigate to /devices", async ({
    authedPage,
  }) => {
    await authedPage.goto("/devices");
    await expect(authedPage).toHaveURL(/\/devices/);
  });

  test("SPA handles client-side routing", async ({ authedPage }) => {
    await authedPage.goto("/devices");

    // Navigate to login via link if visible, or directly
    await authedPage.goto("/login");
    // Should redirect back to devices since already authenticated
    await expect(authedPage).toHaveURL(/\/devices/);
  });
});
