import { test, expect } from "./fixtures";
import { createSite } from "./helpers/api-helper";
import { adminToken, enrolledMachine, MACHINE_B } from "./helpers/enrolled-machine";

// Dragging a machine's card onto a site in the sidebar files it there.
//
// The stack carries two enrolled agents, so the card being dragged belongs to
// a machine that is actually connected and the move is a real one: the browser
// issues the request, the server writes it, and the fleet page reads it back.
// agent-b is the one moved about, because filing a machine changes what every
// later spec's fleet page shows.
//
// Both sites are created and deleted by the spec. A site is visible to the
// whole customer, so one left behind changes what an unrelated later spec
// renders — which is what global-teardown.ts refuses a run for.

const UNFILED = "00000000-0000-0000-0000-000000000000";

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
    // Put the machine back where the rest of the suite expects it, then take
    // the sites away.
    await request.patch(`/api/v1/devices/${machineID}`, {
      data: { site_id: UNFILED },
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

  test("dropping a machine on the Unfiled zone clears its site", async ({ adminPage, request }) => {
    await request.patch(`/api/v1/devices/${machineID}`, {
      data: { site_id: siteA },
      headers: { Authorization: `Bearer ${adminToken()}` },
    });

    await adminPage.goto("/devices");

    const card = adminPage.getByRole("button", { name: new RegExp(machineName) });
    await expect(card).toBeVisible();

    await card.dragTo(adminPage.getByRole("listitem", { name: "Unfiled" }));

    await expect(adminPage.getByText(new RegExp(`Moved ${machineName} to Unfiled`))).toBeVisible();
  });
});
