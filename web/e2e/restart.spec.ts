import { test, expect } from "./fixtures";
import type { Request, Route } from "@playwright/test";

// Restart Agent flow on the DeviceDetail page.
//
// The unit suite (device-store.test.ts, DeviceDetail.test.tsx) covers the store
// action and the button's label states in isolation. This spec exercises the
// route-level integration the unit tests cannot reach: a real click driving
// POST /api/v1/devices/:id/restart and the resulting toast, including the
// two-step confirm guard when sessions are active and the failure path.

const DEVICE_ID = "11111111-1111-4111-8111-aaaaaaaaaaaa";
const GROUP_ID = "33333333-3333-4333-8333-333333333333";

function ok(route: Route, body: unknown) {
  return route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

function fakeDevice() {
  const now = new Date().toISOString();
  return {
    id: DEVICE_ID,
    group_id: GROUP_ID,
    hostname: "e2e-restart-host",
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

function fakeSession() {
  const now = new Date().toISOString();
  return {
    token: "e2e-restart-session-0000000000000000000000000000",
    device_id: DEVICE_ID,
    relay_url: "wss://relay.invalid/relay",
    status: "active",
    created_at: now,
    updated_at: now,
  };
}

type AuthedPage = Parameters<Parameters<typeof test>[2]>[0]["authedPage"];

// stubDetailRoutes wires every endpoint DeviceDetail calls on mount, plus the
// restart endpoint with a caller-supplied status. sessions controls whether the
// device shows active sessions (which arms the two-step restart confirm).
async function stubDetailRoutes(
  page: AuthedPage,
  opts: { sessions?: unknown[]; restartStatus?: number } = {},
) {
  const { sessions = [], restartStatus = 200 } = opts;
  await page.route(`**/api/v1/devices/${DEVICE_ID}`, (route: Route) => ok(route, fakeDevice()));
  await page.route("**/api/v1/groups", (route: Route) =>
    ok(route, [{ id: GROUP_ID, name: "default", owner_id: "x", created_at: "", updated_at: "" }]),
  );
  await page.route(`**/api/v1/sessions?device_id=${DEVICE_ID}*`, (route: Route) =>
    ok(route, sessions),
  );
  await page.route("**/api/v1/amt/devices", (route: Route) => ok(route, []));
  await page.route("**/api/v1/updates/manifests*", (route: Route) => ok(route, []));
  await page.route(`**/api/v1/devices/${DEVICE_ID}/hardware`, (route: Route) =>
    route.fulfill({ status: 404, body: "" }),
  );
  await page.route(`**/api/v1/devices/${DEVICE_ID}/logs*`, (route: Route) =>
    ok(route, { entries: [], total: 0, has_more: false }),
  );
  await page.route(`**/api/v1/devices/${DEVICE_ID}/restart`, (route: Route) => {
    if (route.request().method() !== "POST") return route.fallback();
    if (restartStatus >= 400) {
      // openapi-fetch only populates `error` (which drives the failure path)
      // when the body parses as the ApiError schema, so a JSON body is required.
      return route.fulfill({
        status: restartStatus,
        contentType: "application/json",
        body: JSON.stringify({ error: "agent not connected" }),
      });
    }
    return route.fulfill({ status: restartStatus, body: "" });
  });
}

test.describe("Restart Agent flow", () => {
  test("restart with no active sessions sends immediately and toasts success", async ({
    authedPage,
  }) => {
    await stubDetailRoutes(authedPage);
    await authedPage.goto(`/devices/${DEVICE_ID}`);

    const sent = authedPage.waitForRequest(
      (r: Request) => r.url().includes(`/devices/${DEVICE_ID}/restart`) && r.method() === "POST",
    );
    await authedPage.getByRole("button", { name: "Restart Agent" }).click();
    await sent;

    await expect(authedPage.getByRole("alert").filter({ hasText: "Restart command sent" })).toBeVisible();
  });

  test("active sessions require a second confirm click before sending", async ({
    authedPage,
  }) => {
    await stubDetailRoutes(authedPage, { sessions: [fakeSession()] });
    await authedPage.goto(`/devices/${DEVICE_ID}`);

    let restartPosts = 0;
    await authedPage.route(`**/api/v1/devices/${DEVICE_ID}/restart`, (route: Route) => {
      restartPosts += 1;
      return route.fulfill({ status: 200, body: "" });
    });

    // First click only arms the confirm — no POST yet.
    await authedPage.getByRole("button", { name: "Restart Agent" }).click();
    await expect(authedPage.getByRole("button", { name: /confirm \(1 active\)/i })).toBeVisible();
    expect(restartPosts).toBe(0);

    // Second click sends.
    await authedPage.getByRole("button", { name: /confirm \(1 active\)/i }).click();
    await expect(authedPage.getByRole("alert").filter({ hasText: "Restart command sent" })).toBeVisible();
    expect(restartPosts).toBe(1);
  });

  test("a failed restart surfaces an error toast", async ({ authedPage }) => {
    await stubDetailRoutes(authedPage, { restartStatus: 409 });
    await authedPage.goto(`/devices/${DEVICE_ID}`);

    await authedPage.getByRole("button", { name: "Restart Agent" }).click();
    await expect(authedPage.getByRole("alert").filter({ hasText: "Failed to restart agent" })).toBeVisible();
  });
});
