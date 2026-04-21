import { test, expect } from "./fixtures";
import type { Route } from "@playwright/test";

// These specs verify SessionView tab filtering in availableTabs():
// "Desktop" and "Chat" only render when capabilities include "RemoteDesktop";
// "Terminal" and "Files" always render.
//
// SessionView reads capabilities from React Router `location.state`, which
// DeviceDetail populates on the Start Session click. We drive the same flow
// here by mocking the device + session-create endpoints, then asserting which
// tabs render under each capability set.

const GROUP_ID = "33333333-3333-4333-8333-333333333333";

function deviceId(suffix: number): string {
  return `11111111-1111-4111-8111-00000000000${suffix}`;
}

function fakeDevice(id: string, capabilities: string[]) {
  const now = new Date().toISOString();
  return {
    id,
    group_id: GROUP_ID,
    hostname: `e2e-caps-${capabilities.join("-").toLowerCase() || "none"}`,
    os: "linux",
    os_display: "Linux",
    agent_version: "0.1.0",
    capabilities,
    status: "online",
    last_seen: now,
    created_at: now,
    updated_at: now,
  };
}

function ok(route: Route, body: unknown) {
  return route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function stubDeviceRoutes(
  authedPage: Parameters<Parameters<typeof test>[2]>[0]["authedPage"],
  id: string,
  capabilities: string[],
) {
  await authedPage.route(`**/api/v1/devices/${id}`, (route: Route) =>
    ok(route, fakeDevice(id, capabilities)),
  );
  await authedPage.route("**/api/v1/groups", (route: Route) =>
    ok(route, [
      { id: GROUP_ID, name: "default", owner_id: "x", created_at: "", updated_at: "" },
    ]),
  );
  await authedPage.route(`**/api/v1/sessions?device_id=${id}*`, (route: Route) =>
    ok(route, []),
  );
  await authedPage.route("**/api/v1/amt/devices", (route: Route) =>
    ok(route, []),
  );
  await authedPage.route("**/api/v1/updates/manifests*", (route: Route) =>
    ok(route, []),
  );
  await authedPage.route(`**/api/v1/devices/${id}/hardware`, (route: Route) =>
    route.fulfill({ status: 404, body: "" }),
  );
  // POST /api/v1/sessions returns a token+relay_url so navigate() fires
  // client-side routing to /sessions/:token with capabilities in state.
  await authedPage.route("**/api/v1/sessions", (route: Route) => {
    if (route.request().method() !== "POST") return route.fallback();
    return ok(route, {
      token: "e2e-fake-token-00000000000000000000000000000000",
      relay_url: "wss://relay.invalid/relay",
    });
  });
}

async function openSessionView(
  authedPage: Parameters<Parameters<typeof test>[2]>[0]["authedPage"],
  id: string,
) {
  await authedPage.goto(`/devices/${id}`);
  await authedPage.getByRole("button", { name: /start session/i }).click();
  // Wait for client-side nav with state to reach SessionView (tablist rendered).
  await expect(authedPage.getByRole("tablist")).toBeVisible();
}

test.describe("Session view capability-based tabs", () => {
  test("agent with RemoteDesktop shows Desktop, Terminal, Files and Chat tabs", async ({
    authedPage,
  }) => {
    const id = deviceId(1);
    await stubDeviceRoutes(authedPage, id, [
      "RemoteDesktop",
      "Terminal",
      "FileManager",
    ]);
    await openSessionView(authedPage, id);

    await expect(authedPage.getByRole("tab", { name: "Desktop" })).toBeVisible();
    await expect(authedPage.getByRole("tab", { name: "Terminal" })).toBeVisible();
    await expect(authedPage.getByRole("tab", { name: "Files" })).toBeVisible();
    await expect(authedPage.getByRole("tab", { name: "Chat" })).toBeVisible();
  });

  test("headless agent (no RemoteDesktop) hides Desktop and Chat tabs", async ({
    authedPage,
  }) => {
    const id = deviceId(2);
    await stubDeviceRoutes(authedPage, id, ["Terminal", "FileManager"]);
    await openSessionView(authedPage, id);

    await expect(authedPage.getByRole("tab", { name: "Terminal" })).toBeVisible();
    await expect(authedPage.getByRole("tab", { name: "Files" })).toBeVisible();
    await expect(authedPage.getByRole("tab", { name: "Desktop" })).toHaveCount(0);
    await expect(authedPage.getByRole("tab", { name: "Chat" })).toHaveCount(0);
  });

  test("agent with empty capabilities still shows Terminal and Files", async ({
    authedPage,
  }) => {
    const id = deviceId(3);
    await stubDeviceRoutes(authedPage, id, []);
    await openSessionView(authedPage, id);

    await expect(authedPage.getByRole("tab", { name: "Terminal" })).toBeVisible();
    await expect(authedPage.getByRole("tab", { name: "Files" })).toBeVisible();
    await expect(authedPage.getByRole("tab", { name: "Desktop" })).toHaveCount(0);
    await expect(authedPage.getByRole("tab", { name: "Chat" })).toHaveCount(0);
  });
});
