import { test, expect } from "./fixtures";
import type { Response, Route } from "@playwright/test";

// Discovered-footprint (inventory) render on the DeviceDetail page.
//
// The server-stored WS-17 inventory (GET /api/v1/devices/:id/inventory) renders
// as grouped, sortable tables plus an at-a-glance summary. Server-side ingest
// and RLS scoping are covered by the Go handler/store tests; the store mapping
// and sort behavior by inventory-store.test.ts / DeviceInventory.test.tsx. This
// asserts the fetch-on-mount + render end to end against a stubbed API.

const DEVICE_ID = "11111111-1111-4111-8111-cccccccccccc";
const GROUP_ID = "33333333-3333-4333-8333-333333333333";

function ok(route: Route, body: unknown) {
  return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}

function fakeDevice() {
  const now = new Date().toISOString();
  return {
    id: DEVICE_ID,
    group_id: GROUP_ID,
    hostname: "e2e-inv-host",
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

function fakeInventory() {
  const first = "2026-07-01T00:00:00Z";
  const last = "2026-07-10T00:00:00Z";
  const base = { version: "", proto: "", state: "", runtime: "", image: "", first_seen: first, last_seen: last };
  return {
    device_id: DEVICE_ID,
    items: [
      { ...base, kind: "port", name: "sshd", port: 22, proto: "tcp", state: "LISTEN" },
      { ...base, kind: "port", name: "nginx", port: 443, proto: "tcp", state: "LISTEN" },
      { ...base, kind: "service", name: "cron.service", port: 0, state: "running" },
      { ...base, kind: "db_engine", name: "postgres", version: "17.2", port: 5432 },
      { ...base, kind: "container", name: "web", port: 0, state: "running", runtime: "docker", image: "nginx:latest" },
      { ...base, kind: "package", name: "openssl", version: "3.0.2", port: 0 },
    ],
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
  await page.route(`**/api/v1/devices/${DEVICE_ID}/inventory`, (route: Route) => ok(route, fakeInventory()));
}

test.describe("Discovered footprint", () => {
  test("device detail renders the discovered ports, services, and containers", async ({ authedPage }) => {
    await stubDetailRoutes(authedPage);

    const fetched = authedPage.waitForResponse(
      (r: Response) => r.url().includes(`/devices/${DEVICE_ID}/inventory`) && r.status() === 200,
    );
    await authedPage.goto(`/devices/${DEVICE_ID}`);
    await fetched;

    // Grouped tables, one per discovered kind, with counts.
    await expect(authedPage.getByText("Listening Ports (2)")).toBeVisible();
    await expect(authedPage.getByText("Services (1)")).toBeVisible();
    await expect(authedPage.getByText("Containers (1)")).toBeVisible();
    // Verbatim discovered values render.
    await expect(authedPage.getByText("sshd")).toBeVisible();
    await expect(authedPage.getByText("nginx:latest")).toBeVisible();
    await expect(authedPage.getByText("postgres")).toBeVisible();
    // At-a-glance summary for a freshly enrolled host.
    await expect(authedPage.getByText(/^Discovered:/)).toContainText("2 listening ports");
  });
});
