import { test, expect } from "./fixtures";
import type { Route } from "@playwright/test";

// These specs exercise the Device Logs UI on the DeviceDetail page.
// Logs are retrieved via an asynchronous agent round-trip
// (server -> agent control channel -> agent collects logs -> server caches -> UI),
// so there is no public API for a test to seed logs directly.
// We therefore intercept /api/v1/devices/:id/logs with Playwright and assert
// that the UI renders, filters, and paginates correctly against deterministic payloads.

const DEVICE_ID = "11111111-1111-4111-8111-111111111111";
const GROUP_ID = "22222222-2222-4222-8222-222222222222";
const LEVELS = ["INFO", "WARN", "ERROR"] as const;

function fakeDevice() {
  const now = new Date().toISOString();
  return {
    id: DEVICE_ID,
    group_id: GROUP_ID,
    hostname: "e2e-log-host",
    os: "linux",
    os_display: "Linux",
    agent_version: "0.1.0",
    capabilities: ["RemoteDesktop", "Terminal", "FileManager"],
    status: "online",
    last_seen: now,
    created_at: now,
    updated_at: now,
  };
}

function pickLevel(level: string | undefined, i: number): string {
  if (level) return level;
  return LEVELS[i % LEVELS.length];
}

function fakeLogs(count: number, level?: string) {
  const entries = Array.from({ length: count }, (_, i) => ({
    timestamp: new Date(1_700_000_000_000 + i * 1000).toISOString(),
    level: pickLevel(level, i),
    target: "mesh_agent::core",
    message: `synthetic log message #${i}`,
  }));
  return { entries, total: count, has_more: false };
}

function ok(route: Route, body: unknown) {
  return route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

test.describe("Device logs UI", () => {
  test.beforeEach(async ({ authedPage }) => {
    // Backing collections for endpoints DeviceDetail calls on mount.
    await authedPage.route(`**/api/v1/devices/${DEVICE_ID}`, (route: Route) =>
      ok(route, fakeDevice()),
    );
    await authedPage.route("**/api/v1/groups", (route: Route) =>
      ok(route, [
        { id: GROUP_ID, name: "default", owner_id: "x", created_at: "", updated_at: "" },
      ]),
    );
    await authedPage.route(
      `**/api/v1/sessions?device_id=${DEVICE_ID}*`,
      (route: Route) => ok(route, []),
    );
    await authedPage.route("**/api/v1/amt/devices", (route: Route) =>
      ok(route, []),
    );
    await authedPage.route("**/api/v1/updates/manifests*", (route: Route) =>
      ok(route, []),
    );
    await authedPage.route(
      `**/api/v1/devices/${DEVICE_ID}/hardware`,
      (route: Route) => route.fulfill({ status: 404, body: "" }),
    );
  });

  test("renders fetched logs and paginates via Load More", async ({
    authedPage,
  }) => {
    let callCount = 0;
    await authedPage.route(
      `**/api/v1/devices/${DEVICE_ID}/logs*`,
      (route: Route) => {
        callCount += 1;
        const url = new URL(route.request().url());
        const offset = Number.parseInt(url.searchParams.get("offset") ?? "0", 10);
        const limit = Number.parseInt(url.searchParams.get("limit") ?? "300", 10);
        const start = offset;
        const total = 500;
        const slice = Math.min(limit, total - start);
        const entries = Array.from({ length: slice }, (_, i) => ({
          timestamp: new Date(1_700_000_000_000 + (start + i) * 1000).toISOString(),
          level: i === 0 ? "WARN" : "INFO",
          target: "mesh_agent::core",
          message: `entry #${start + i}`,
        }));
        return ok(route, { entries, total, has_more: start + slice < total });
      },
    );

    await authedPage.goto(`/devices/${DEVICE_ID}`);
    await authedPage.getByRole("button", { name: /fetch logs/i }).click();

    await expect(authedPage.getByText("entry #0")).toBeVisible();
    await expect(authedPage.getByText(/Showing 1-300 of 500/)).toBeVisible();

    await authedPage.getByRole("button", { name: /load more/i }).click();

    await expect(authedPage.getByText("entry #300")).toBeVisible();
    await expect(authedPage.getByText(/Showing 301-500 of 500/)).toBeVisible();

    expect(callCount).toBeGreaterThanOrEqual(2);
  });

  test("level filter is sent to the server and reflected in the UI", async ({
    authedPage,
  }) => {
    const levelsRequested: string[] = [];
    await authedPage.route(
      `**/api/v1/devices/${DEVICE_ID}/logs*`,
      (route: Route) => {
        const url = new URL(route.request().url());
        const level = url.searchParams.get("level") ?? "";
        if (level) levelsRequested.push(level);
        return ok(route, fakeLogs(5, level || "INFO"));
      },
    );

    await authedPage.goto(`/devices/${DEVICE_ID}`);

    await authedPage.getByRole("combobox").first().selectOption("ERROR");
    await authedPage.getByRole("button", { name: /fetch logs/i }).click();

    await expect(authedPage.getByText("synthetic log message #0")).toBeVisible();
    // Every rendered row must carry the requested level.
    const rows = authedPage.locator("table tbody tr");
    const count = await rows.count();
    expect(count).toBe(5);
    for (let i = 0; i < count; i += 1) {
      await expect(rows.nth(i)).toContainText("ERROR");
    }
    expect(levelsRequested).toContain("ERROR");
  });

  test("empty logs show no-logs message", async ({ authedPage }) => {
    await authedPage.route(
      `**/api/v1/devices/${DEVICE_ID}/logs*`,
      (route: Route) => ok(route, { entries: [], total: 0, has_more: false }),
    );

    await authedPage.goto(`/devices/${DEVICE_ID}`);
    await authedPage.getByRole("button", { name: /fetch logs/i }).click();

    await expect(authedPage.getByText(/no logs available/i)).toBeVisible();
  });
});
