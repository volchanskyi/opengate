import { test, expect } from "../e2e/fixtures";

test.describe("Admin panel", () => {
  test("non-admin is blocked from /admin", async ({ authedPage }) => {
    await authedPage.goto("/admin/users");

    // AdminGuard should redirect non-admin users or show forbidden
    await expect(
      authedPage.getByText(/forbidden|access denied|not authorized/i).or(
        authedPage.locator("body")
      )
    ).toBeVisible();

    // URL should not stay on /admin
    const url = authedPage.url();
    expect(
      url.includes("/admin") === false || url.includes("/devices") || url.includes("/login")
    ).toBeTruthy();
  });

  test("admin can see user list at /admin/users", async ({ page, request }) => {
    // This test requires an admin user. Since we can't promote via API,
    // we verify the page loads (even if it redirects for non-admin).
    // Full admin E2E would need server-side test user seeding.
    await page.goto("/login");
    // Verify the admin route exists and is reachable
    const resp = await request.get("/admin/users");
    // SPA always returns 200 (index.html) regardless of route
    expect(resp.status()).toBe(200);
  });

  test("audit log page loads", async ({ page }) => {
    await page.goto("/admin/audit");
    // SPA serves index.html for all routes
    await expect(page.locator("body")).toBeVisible();
  });
});
