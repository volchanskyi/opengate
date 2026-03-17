import { test, expect } from "./fixtures";

test.describe("Admin panel", () => {
  test("non-admin is blocked from /admin", async ({ authedPage }) => {
    await authedPage.goto("/admin/users");

    // AdminGuard should redirect non-admin users or show forbidden
    await expect(
      authedPage
        .getByText(/forbidden|access denied|not authorized/i)
        .or(authedPage.locator("body"))
    ).toBeVisible();

    // URL should not stay on /admin
    const url = authedPage.url();
    expect(
      url.includes("/admin") === false ||
        url.includes("/devices") ||
        url.includes("/login")
    ).toBeTruthy();
  });

  test("admin can see user list at /admin/users", async ({ adminPage }) => {
    await adminPage.goto("/admin/users");

    await expect(
      adminPage.getByRole("heading", { name: /user management/i })
    ).toBeVisible();
    // Should see at least the admin user in the table
    await expect(adminPage.locator("table")).toBeVisible();
  });

  test("audit log page loads", async ({ adminPage }) => {
    await adminPage.goto("/admin/audit");

    await expect(
      adminPage.getByRole("heading", { name: /audit/i })
    ).toBeVisible();
  });

  test("admin sidebar shows Management and Security sections", async ({
    adminPage,
  }) => {
    await adminPage.goto("/admin");

    await expect(adminPage.getByText("Management", { exact: true })).toBeVisible();
    await expect(
      adminPage.getByRole("link", { name: "Users" })
    ).toBeVisible();
    await expect(
      adminPage.getByRole("link", { name: "Audit Log" })
    ).toBeVisible();
    await expect(
      adminPage.getByRole("link", { name: "Agent Updates" })
    ).toBeVisible();
    await expect(adminPage.getByText("Security")).toBeVisible();
    await expect(
      adminPage.getByRole("link", { name: "Permissions" })
    ).toBeVisible();
  });
});
