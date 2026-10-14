import { test, expect } from "./fixtures";
import type { Route } from "@playwright/test";
import { openSessionTab, stubSessionStart } from "./helpers/session-tab";

// The session tab reads the host's capabilities from its device record, stubbed on the context.

const GROUP_ID = "33333333-3333-4333-8333-333333333333";

function deviceId(suffix: number): string {
  return `11111111-1111-4111-8111-00000000000${suffix}`;
}

function fakeDevice(id: string, capabilities: string[]) {
  const now = new Date().toISOString();
  return {
    id,
    site_id: GROUP_ID,
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
  const context = authedPage.context();
  await stubSessionStart(context, test.info().project.use.baseURL!);
  // A relay that accepts and stays silent keeps the session open with its tabs shown.
  await context.routeWebSocket((url: URL) => url.pathname.includes("/relay"), () => {});
  await context.route(`**/api/v1/devices/${id}`, (route: Route) =>
    ok(route, fakeDevice(id, capabilities)),
  );
  await context.route("**/api/v1/sites", (route: Route) =>
    ok(route, [
      { id: GROUP_ID, name: "default", created_at: "", updated_at: "" },
    ]),
  );
  await context.route(`**/api/v1/sessions?device_id=${id}*`, (route: Route) =>
    ok(route, []),
  );
  await context.route("**/api/v1/amt/devices", (route: Route) =>
    ok(route, []),
  );
  await context.route("**/api/v1/updates/manifests*", (route: Route) =>
    ok(route, []),
  );
  await context.route(`**/api/v1/devices/${id}/hardware`, (route: Route) =>
    route.fulfill({ status: 404, body: "" }),
  );
}

async function openSessionView(
  authedPage: Parameters<Parameters<typeof test>[2]>[0]["authedPage"],
  id: string,
) {
  const tab = await openSessionTab(authedPage, id);
  await expect(tab.getByRole("tablist")).toBeVisible();
  return tab;
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
    const tab = await openSessionView(authedPage, id);

    await expect(tab.getByRole("tab", { name: "Desktop" })).toBeVisible();
    await expect(tab.getByRole("tab", { name: "Terminal" })).toBeVisible();
    await expect(tab.getByRole("tab", { name: "Files" })).toBeVisible();
    await expect(tab.getByRole("tab", { name: "Chat" })).toBeVisible();
  });

  test("headless agent (no RemoteDesktop) hides Desktop and Chat tabs", async ({
    authedPage,
  }) => {
    const id = deviceId(2);
    await stubDeviceRoutes(authedPage, id, ["Terminal", "FileManager"]);
    const tab = await openSessionView(authedPage, id);

    await expect(tab.getByRole("tab", { name: "Terminal" })).toBeVisible();
    await expect(tab.getByRole("tab", { name: "Files" })).toBeVisible();
    await expect(tab.getByRole("tab", { name: "Desktop" })).toHaveCount(0);
    await expect(tab.getByRole("tab", { name: "Chat" })).toHaveCount(0);
  });

  test("agent with empty capabilities still shows Terminal and Files", async ({
    authedPage,
  }) => {
    const id = deviceId(3);
    await stubDeviceRoutes(authedPage, id, []);
    const tab = await openSessionView(authedPage, id);

    await expect(tab.getByRole("tab", { name: "Terminal" })).toBeVisible();
    await expect(tab.getByRole("tab", { name: "Files" })).toBeVisible();
    await expect(tab.getByRole("tab", { name: "Desktop" })).toHaveCount(0);
    await expect(tab.getByRole("tab", { name: "Chat" })).toHaveCount(0);
  });
});
