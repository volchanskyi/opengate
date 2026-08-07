import { test, expect } from "./fixtures";
import type { APIRequestContext } from "@playwright/test";
import { createSite } from "./helpers/api-helper";
import { stubEmptyFleet } from "./helpers/fleet-stub";

// Sites are visible to every member of the organization, so a site left
// behind here would show up in another spec's "empty device list" assertion —
// and in its screenshot baseline. Each test registers what it created and the
// hook removes it, pass or fail.
const createdGroupIds: string[] = [];

async function seedGroup(
  request: APIRequestContext,
  adminToken: string,
  name: string,
): Promise<void> {
  const site = await createSite(request, adminToken, name);
  createdGroupIds.push(site.id);
}

test.describe("Device list", () => {
  test.afterEach(async ({ request, adminUser }) => {
    for (const id of createdGroupIds.splice(0)) {
      await request.delete(`/api/v1/sites/${id}`, {
        headers: { Authorization: `Bearer ${adminUser.token}` },
      });
    }
  });

  test("empty state shows no sites message", async ({ authedPage }) => {
    // Emptiness is the precondition under test, so this test supplies it rather
    // than inheriting whatever the suite has already seeded into the shared
    // organization.
    await stubEmptyFleet(authedPage);
    await authedPage.goto("/devices");

    await expect(authedPage.getByText("No sites yet")).toBeVisible();
    await expect(authedPage.getByText("Welcome to OpenGate")).toBeVisible();
  });

  // Creating a site is admin-only; seeing it is not. Both tests seed with the
  // admin token and then assert against an ordinary member's page.
  test("created site appears in sidebar", async ({
    authedPage,
    adminUser,
    request,
  }) => {
    const groupName = `e2e-site-${Date.now()}`;
    await seedGroup(request, adminUser.token, groupName);

    await authedPage.goto("/devices");
    await authedPage.reload();

    await expect(authedPage.getByText(groupName)).toBeVisible();
  });

  test("selected site shows empty device list", async ({
    authedPage,
    adminUser,
    request,
  }) => {
    const groupName = `e2e-empty-${Date.now()}`;
    await seedGroup(request, adminUser.token, groupName);

    await authedPage.goto("/devices");
    await authedPage.reload();

    // Click the site in sidebar
    await authedPage.getByText(groupName).click();

    // Should show empty device list for that site
    await expect(authedPage.getByText(/no devices/i)).toBeVisible();
  });
});
