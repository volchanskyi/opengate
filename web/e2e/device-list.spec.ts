import { test, expect } from "../e2e/fixtures";
import { createGroup } from "./helpers/api-helper";

test.describe("Device list", () => {
  test("empty state shows no groups message", async ({ authedPage }) => {
    await authedPage.goto("/devices");

    // With no groups, the sidebar or main area should indicate empty state
    await expect(authedPage.getByText(/no groups|no devices|create.*group/i)).toBeVisible();
  });

  test("created group appears in sidebar", async ({
    authedPage,
    testUser,
    request,
  }) => {
    const groupName = `e2e-group-${Date.now()}`;
    await createGroup(request, testUser.token, groupName);

    await authedPage.goto("/devices");
    await authedPage.reload();

    await expect(authedPage.getByText(groupName)).toBeVisible();
  });

  test("selected group shows empty device list", async ({
    authedPage,
    testUser,
    request,
  }) => {
    const groupName = `e2e-empty-${Date.now()}`;
    await createGroup(request, testUser.token, groupName);

    await authedPage.goto("/devices");
    await authedPage.reload();

    // Click the group in sidebar
    await authedPage.getByText(groupName).click();

    // Should show empty device list for that group
    await expect(authedPage.getByText(/no devices/i)).toBeVisible();
  });
});
