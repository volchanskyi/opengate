import { test, expect } from "./fixtures";
import { createSite } from "./helpers/api-helper";
import { adminToken, enrolledMachine, MACHINE_A, MACHINE_B } from "./helpers/enrolled-machine";

// A site is visible to the whole customer, so the spec deletes both sites it creates.

const NOT_ASSIGNED = "00000000-0000-0000-0000-000000000000";

test.describe("Device site drag and drop", () => {
  let siteA = "";
  let siteB = "";
  let machineID = "";
  let machineName = "";

  test.beforeEach(async ({ request }) => {
    const token = adminToken();
    siteA = (await createSite(request, token, "Site A")).id;
    siteB = (await createSite(request, token, "Site B")).id;

    const machine = await enrolledMachine(request, MACHINE_B);
    machineID = machine.id;
    machineName = machine.hostname;
  });

  test.afterEach(async ({ request }) => {
    const headers = { Authorization: `Bearer ${adminToken()}` };
    // Returns the machine to the no-site state the rest of the suite expects.
    await request.patch(`/api/v1/devices/${machineID}`, {
      data: { site_id: NOT_ASSIGNED },
      headers,
    });
    for (const site of [siteA, siteB]) {
      if (site) await request.delete(`/api/v1/sites/${site}`, { headers });
    }
  });

  test("dropping a machine's card on a site files it there", async ({ adminPage }) => {
    await adminPage.goto("/devices");

    const card = adminPage.getByRole("button", { name: new RegExp(machineName) });
    await expect(card).toBeVisible();

    await card.dragTo(adminPage.getByRole("listitem", { name: "Site B" }));

    await expect(adminPage.getByText(new RegExp(`Moved ${machineName} to Site B`))).toBeVisible();
  });

  test("dropping a machine on Not Assigned clears its site", async ({ adminPage, request }) => {
    await request.patch(`/api/v1/devices/${machineID}`, {
      data: { site_id: siteA },
      headers: { Authorization: `Bearer ${adminToken()}` },
    });

    await adminPage.goto("/devices");

    const card = adminPage.getByRole("button", { name: new RegExp(machineName) });
    await expect(card).toBeVisible();

    await card.dragTo(adminPage.getByRole("listitem", { name: "Not Assigned" }));

    await expect(adminPage.getByText(new RegExp(`Moved ${machineName} to Not Assigned`))).toBeVisible();
  });

  test("Not Assigned lists only the devices filed under no site, and Show All Devices brings back the rest", async ({
    authedPage,
    request,
  }) => {
    const unfiled = await enrolledMachine(request, MACHINE_A);
    await request.patch(`/api/v1/devices/${machineID}`, {
      data: { site_id: siteA },
      headers: { Authorization: `Bearer ${adminToken()}` },
    });

    await authedPage.goto("/devices");
    const filedCard = authedPage.getByRole("button", { name: new RegExp(machineName) });
    const unfiledCard = authedPage.getByRole("button", { name: new RegExp(unfiled.hostname) });
    await expect(filedCard).toBeVisible();
    await expect(authedPage.getByRole("button", { name: "Show All Devices" })).toBeDisabled();

    await authedPage.getByRole("button", { name: "Not Assigned" }).click();
    await expect(unfiledCard).toBeVisible();
    await expect(filedCard).toHaveCount(0);

    await authedPage.getByRole("button", { name: "Show All Devices" }).click();
    await expect(filedCard).toBeVisible();
    await expect(unfiledCard).toBeVisible();
    await expect(authedPage.getByRole("button", { name: "Show All Devices" })).toBeDisabled();
  });
});
