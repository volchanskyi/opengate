import { test, expect } from "./fixtures";
import {
  register,
  getMe,
  getSecurityGroup,
  addGroupMember,
  removeGroupMember,
} from "./helpers/api-helper";

const ADMIN_GROUP_ID = "00000000-0000-0000-0000-000000000001";

test.describe("Security Permissions", () => {
  test("admin sees Security > Permissions in sidebar", async ({
    adminPage,
  }) => {
    await adminPage.goto("/settings");

    // Sidebar should have Security section header and Permissions link
    await expect(adminPage.getByText("Security")).toBeVisible();
    await expect(
      adminPage.getByRole("link", { name: "Permissions" })
    ).toBeVisible();
  });

  test("Permissions page shows Administrators group with System badge", async ({
    adminPage,
  }) => {
    await adminPage.goto("/settings/security/permissions");

    await expect(
      adminPage.getByRole("heading", { name: "Permissions" })
    ).toBeVisible();
    await expect(
      adminPage.getByRole("button", { name: /Administrators/i })
    ).toBeVisible();
    await expect(adminPage.getByText("System", { exact: true })).toBeVisible();
  });

  test("admin sees themselves in Administrators group", async ({
    adminPage,
    adminUser,
  }) => {
    await adminPage.goto("/settings/security/permissions");

    // The admin's email should appear in the members table
    await expect(adminPage.getByRole('cell', { name: adminUser.email })).toBeVisible();
  });

  test("admin can add a user to Administrators via API", async ({
    request,
    adminUser,
  }) => {
    // Register a second (non-admin) user
    const email = `e2e-perm-add-${Date.now()}@test.local`;
    const regularToken = await register(request, email, "TestPass123!");
    const regularMe = await getMe(request, regularToken);

    // Add to Administrators group
    await addGroupMember(
      request,
      adminUser.token,
      ADMIN_GROUP_ID,
      regularMe.id
    );

    // Verify membership
    const group = await getSecurityGroup(
      request,
      adminUser.token,
      ADMIN_GROUP_ID
    );
    const memberEmails = group.members.map((m) => m.email);
    expect(memberEmails).toContain(email);
  });

  test("admin can remove a user from Administrators via API", async ({
    request,
    adminUser,
  }) => {
    // Register and add a second user
    const email = `e2e-perm-rm-${Date.now()}@test.local`;
    const regularToken = await register(request, email, "TestPass123!");
    const regularMe = await getMe(request, regularToken);
    await addGroupMember(
      request,
      adminUser.token,
      ADMIN_GROUP_ID,
      regularMe.id
    );

    // Remove them
    await removeGroupMember(
      request,
      adminUser.token,
      ADMIN_GROUP_ID,
      regularMe.id
    );

    // Verify removed
    const group = await getSecurityGroup(
      request,
      adminUser.token,
      ADMIN_GROUP_ID
    );
    const memberIds = group.members.map((m) => m.id);
    expect(memberIds).not.toContain(regularMe.id);
  });

  test("cannot remove last admin via API", async ({ request, adminUser }) => {
    // List current admin group members and remove all except adminUser
    const group = await getSecurityGroup(request, adminUser.token, ADMIN_GROUP_ID);
    for (const member of group.members) {
      if (member.id !== adminUser.id) {
        await removeGroupMember(request, adminUser.token, ADMIN_GROUP_ID, member.id);
      }
    }

    // Now adminUser is the only admin — removing should fail
    const resp = await request.delete(
      `/api/v1/security-groups/${ADMIN_GROUP_ID}/members/${adminUser.id}`,
      { headers: { Authorization: `Bearer ${adminUser.token}` } }
    );
    expect(resp.status()).toBe(409);
  });
});
