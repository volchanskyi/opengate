import { test, expect } from "./fixtures";
import type { Route } from "@playwright/test";

// Dragging a device card onto a site in the sidebar re-sites it. Devices only
// exist after an agent enrolls, and there is no public API to seed one, so the
// device list and the PATCH that moves it are stubbed with Playwright and the
// assertions target the request the UI actually issues.

const DEVICE_ID = "33333333-3333-4333-8333-333333333333";
const GROUP_A = "44444444-4444-4444-8444-444444444444";
const GROUP_B = "55555555-5555-4555-8555-555555555555";
const UNGROUPED = "00000000-0000-0000-0000-000000000000";

function ok(route: Route, body: unknown) {
  return route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

function fakeDevice(siteId: string) {
  const now = new Date().toISOString();
  return {
    id: DEVICE_ID,
    site_id: siteId,
    hostname: "e2e-dnd-host",
    os: "linux",
    os_display: "Linux",
    agent_version: "0.1.0",
    capabilities: [],
    status: "online",
    last_seen: now,
    created_at: now,
    updated_at: now,
  };
}

/** Stub the device list, sites and the PATCH; returns the captured move bodies. */
async function stubList(page: import("@playwright/test").Page) {
  const moves: { id: string; site_id: string }[] = [];
  let currentGroup = GROUP_A;

  await page.route("**/api/v1/sites", (route: Route) =>
    ok(route, [
      { id: GROUP_A, name: "Site A", created_at: "", updated_at: "" },
      { id: GROUP_B, name: "Site B", created_at: "", updated_at: "" },
    ]),
  );
  await page.route("**/api/v1/updates/manifests*", (route: Route) => ok(route, []));
  await page.route(`**/api/v1/devices/${DEVICE_ID}/inventory*`, (route: Route) => ok(route, []));
  await page.route(`**/api/v1/devices/${DEVICE_ID}`, (route: Route) => {
    if (route.request().method() === "PATCH") {
      const body = route.request().postDataJSON() as { site_id: string };
      moves.push({ id: DEVICE_ID, site_id: body.site_id });
      currentGroup = body.site_id;
      return ok(route, fakeDevice(currentGroup));
    }
    return ok(route, fakeDevice(currentGroup));
  });
  await page.route("**/api/v1/devices?**", (route: Route) => ok(route, [fakeDevice(currentGroup)]));
  await page.route("**/api/v1/devices", (route: Route) => ok(route, [fakeDevice(currentGroup)]));

  return moves;
}

test.describe("Device site drag and drop", () => {
  test("dropping a device card on a site moves it there", async ({ adminPage }) => {
    const moves = await stubList(adminPage);
    await adminPage.goto("/devices");

    const card = adminPage.getByRole("button", { name: /e2e-dnd-host/ });
    await expect(card).toBeVisible();

    await card.dragTo(adminPage.getByRole("listitem", { name: "Site B" }));

    await expect(adminPage.getByText(/Moved e2e-dnd-host to Site B/)).toBeVisible();
    expect(moves).toEqual([{ id: DEVICE_ID, site_id: GROUP_B }]);
  });

  test("dropping a device on the Unfiled zone clears its site", async ({ adminPage }) => {
    const moves = await stubList(adminPage);
    await adminPage.goto("/devices");

    const card = adminPage.getByRole("button", { name: /e2e-dnd-host/ });
    await expect(card).toBeVisible();

    await card.dragTo(adminPage.getByRole("listitem", { name: "Unfiled" }));

    await expect(adminPage.getByText(/Moved e2e-dnd-host to Unfiled/)).toBeVisible();
    expect(moves).toEqual([{ id: DEVICE_ID, site_id: UNGROUPED }]);
  });
});
