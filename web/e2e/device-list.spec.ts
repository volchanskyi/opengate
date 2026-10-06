import { test, expect } from "./fixtures";
import type { APIRequestContext } from "@playwright/test";
import { createSite } from "./helpers/api-helper";
import { stubEmptyFleet } from "./helpers/fleet-stub";

// Sites are visible to the whole organization, so each test removes the sites it creates.
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
    await stubEmptyFleet(authedPage);
    await authedPage.goto("/devices");

    await expect(authedPage.getByText("No sites yet")).toBeVisible();
    await expect(authedPage.getByText("Welcome to OpenGate")).toBeVisible();
  });

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

    await authedPage.getByText(groupName).click();

    await expect(authedPage.getByText(/no devices/i)).toBeVisible();
  });
});
