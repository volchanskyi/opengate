import { test, expect } from "./fixtures";
import type { APIRequestContext } from "@playwright/test";
import { createGroup } from "./helpers/api-helper";
import { stubEmptyFleet } from "./helpers/fleet-stub";

// Groups are visible to every member of the organization, so a group left
// behind here would show up in another spec's "empty device list" assertion —
// and in its screenshot baseline. Each test registers what it created and the
// hook removes it, pass or fail.
const createdGroupIds: string[] = [];

async function seedGroup(
  request: APIRequestContext,
  adminToken: string,
  name: string,
): Promise<void> {
  const group = await createGroup(request, adminToken, name);
  createdGroupIds.push(group.id);
}

test.describe("Device list", () => {
  test.afterEach(async ({ request, adminUser }) => {
    for (const id of createdGroupIds.splice(0)) {
      await request.delete(`/api/v1/groups/${id}`, {
        headers: { Authorization: `Bearer ${adminUser.token}` },
      });
    }
  });

  test("empty state shows no groups message", async ({ authedPage }) => {
    // Emptiness is the precondition under test, so this test supplies it rather
    // than inheriting whatever the suite has already seeded into the shared
    // organization.
    await stubEmptyFleet(authedPage);
    await authedPage.goto("/devices");

    await expect(authedPage.getByText("No groups yet")).toBeVisible();
    await expect(authedPage.getByText("Welcome to OpenGate")).toBeVisible();
  });

  // Creating a group is admin-only; seeing it is not. Both tests seed with the
  // admin token and then assert against an ordinary member's page.
  test("created group appears in sidebar", async ({
    authedPage,
    adminUser,
    request,
  }) => {
    const groupName = `e2e-group-${Date.now()}`;
    await seedGroup(request, adminUser.token, groupName);

    await authedPage.goto("/devices");
    await authedPage.reload();

    await expect(authedPage.getByText(groupName)).toBeVisible();
  });

  test("selected group shows empty device list", async ({
    authedPage,
    adminUser,
    request,
  }) => {
    const groupName = `e2e-empty-${Date.now()}`;
    await seedGroup(request, adminUser.token, groupName);

    await authedPage.goto("/devices");
    await authedPage.reload();

    // Click the group in sidebar
    await authedPage.getByText(groupName).click();

    // Should show empty device list for that group
    await expect(authedPage.getByText(/no devices/i)).toBeVisible();
  });
});
