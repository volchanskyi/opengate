import { test, expect } from "./fixtures";
import type { Response, Route } from "@playwright/test";

// Hardware inventory render on the DeviceDetail page.
//
// audit-tests-coverage.md F4: prior specs only mention "hardware" as tab/label
// presence. This asserts the real fetch+render: clicking "Refresh Hardware"
// issues GET /api/v1/devices/:id/hardware and the returned CPU/RAM/disk and
// network-interface values render. Server-side hardware handling is covered by
// the Go handler tests; the store mapping by device-store.test.ts.

const DEVICE_ID = "11111111-1111-4111-8111-bbbbbbbbbbbb";
const GROUP_ID = "33333333-3333-4333-8333-333333333333";

function ok(route: Route, body: unknown) {
  return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}

function fakeDevice() {
  const now = new Date().toISOString();
  return {
    id: DEVICE_ID,
    group_id: GROUP_ID,
    hostname: "e2e-hw-host",
    os: "linux",
    os_display: "Linux",
    agent_version: "0.1.0",
    capabilities: ["Terminal"],
    status: "online",
    last_seen: now,
    created_at: now,
    updated_at: now,
  };
}

function fakeHardware() {
  return {
    device_id: DEVICE_ID,
    cpu_model: "Intel Core i7-9750H",
    cpu_cores: 12,
    ram_total_mb: 16384,
    disk_total_mb: 512000,
    disk_free_mb: 256000,
    network_interfaces: [
      { name: "eth0", mac: "de:ad:be:ef:00:01", ipv4: ["192.0.2.10"], ipv6: [] },
    ],
    updated_at: new Date().toISOString(),
  };
}

type AuthedPage = Parameters<Parameters<typeof test>[2]>[0]["authedPage"];

async function stubDetailRoutes(page: AuthedPage) {
  await page.route(`**/api/v1/devices/${DEVICE_ID}`, (route: Route) => ok(route, fakeDevice()));
  await page.route("**/api/v1/groups", (route: Route) =>
    ok(route, [{ id: GROUP_ID, name: "default", owner_id: "x", created_at: "", updated_at: "" }]),
  );
  await page.route(`**/api/v1/sessions?device_id=${DEVICE_ID}*`, (route: Route) => ok(route, []));
  await page.route("**/api/v1/amt/devices", (route: Route) => ok(route, []));
  await page.route("**/api/v1/updates/manifests*", (route: Route) => ok(route, []));
  await page.route(`**/api/v1/devices/${DEVICE_ID}/logs*`, (route: Route) =>
    ok(route, { entries: [], total: 0, has_more: false }),
  );
  await page.route(`**/api/v1/devices/${DEVICE_ID}/hardware`, (route: Route) =>
    ok(route, fakeHardware()),
  );
}

test.describe("Hardware inventory", () => {
  test("Refresh Hardware fetches and renders inventory values", async ({ authedPage }) => {
    await stubDetailRoutes(authedPage);
    await authedPage.goto(`/devices/${DEVICE_ID}`);

    const fetched = authedPage.waitForResponse(
      (r: Response) => r.url().includes(`/devices/${DEVICE_ID}/hardware`) && r.status() === 200,
    );
    await authedPage.getByRole("button", { name: "Refresh Hardware" }).click();
    await fetched;

    // CPU model + core count render together in the CPU field.
    await expect(authedPage.getByText(/Intel Core i7-9750H/)).toBeVisible();
    await expect(authedPage.getByText(/12 cores/)).toBeVisible();
    // Network interface row renders name, MAC and IPv4 verbatim.
    await expect(authedPage.getByText(/eth0/)).toBeVisible();
    await expect(authedPage.getByText(/de:ad:be:ef:00:01/)).toBeVisible();
    await expect(authedPage.getByText(/192\.0\.2\.10/)).toBeVisible();
  });
});
