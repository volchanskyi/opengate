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

    await expect(adminPage.getByRole('cell', { name: adminUser.email })).toBeVisible();
  });

  test("admin can add a user to Administrators via API", async ({
    request,
    adminUser,
  }) => {
    const email = `e2e-perm-add-${Date.now()}@test.local`;
    const regularToken = await register(request, email, "TestPass123!");
    const regularMe = await getMe(request, regularToken);

    await addGroupMember(
      request,
      adminUser.token,
      ADMIN_GROUP_ID,
      regularMe.id
    );

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
    const email = `e2e-perm-rm-${Date.now()}@test.local`;
    const regularToken = await register(request, email, "TestPass123!");
    const regularMe = await getMe(request, regularToken);
    await addGroupMember(
      request,
      adminUser.token,
      ADMIN_GROUP_ID,
      regularMe.id
    );

    await removeGroupMember(
      request,
      adminUser.token,
      ADMIN_GROUP_ID,
      regularMe.id
    );

    const group = await getSecurityGroup(
      request,
      adminUser.token,
      ADMIN_GROUP_ID
    );
    const memberIds = group.members.map((m) => m.id);
    expect(memberIds).not.toContain(regularMe.id);
  });

  test("cannot remove last admin via API", async ({ request, adminUser }) => {
    // Emptying the group strips the bootstrap admin the suite depends on, so the finally restores it.
    const group = await getSecurityGroup(request, adminUser.token, ADMIN_GROUP_ID);
    const displaced = group.members.filter((m) => m.id !== adminUser.id);

    try {
      for (const member of displaced) {
        await removeGroupMember(request, adminUser.token, ADMIN_GROUP_ID, member.id);
      }

      const resp = await request.delete(
        `/api/v1/security-groups/${ADMIN_GROUP_ID}/members/${adminUser.id}`,
        { headers: { Authorization: `Bearer ${adminUser.token}` } }
      );
      expect(resp.status()).toBe(409);
    } finally {
      for (const member of displaced) {
        await addGroupMember(request, adminUser.token, ADMIN_GROUP_ID, member.id);
      }
    }
  });
});
