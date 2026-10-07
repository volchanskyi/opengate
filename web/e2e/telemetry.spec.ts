import { test, expect } from "./fixtures";
import type { Route } from "@playwright/test";

// VictoriaMetrics is not seeded in the e2e stack, so the metric window and logs are intercepted.

const DEVICE_ID = "33333333-3333-4333-8333-333333333333";
const GROUP_ID = "44444444-4444-4444-8444-444444444444";

function fakeDevice() {
  const now = new Date().toISOString();
  return {
    id: DEVICE_ID,
    site_id: GROUP_ID,
    hostname: "e2e-metrics-host",
    os: "linux",
    os_display: "Linux",
    agent_version: "0.1.0",
    capabilities: ["RemoteDesktop", "Terminal"],
    status: "online",
    last_seen: now,
    created_at: now,
    updated_at: now,
    anomaly_rate: 0.8,
  };
}

function fakeMetrics() {
  const now = Math.floor(Date.now() / 1000);
  const t = Array.from({ length: 30 }, (_, i) => now - (29 - i) * 60);
  const avg = t.map((_, i) => 20 + (i % 5) * 6);
  const min = avg.map((v) => v - 4);
  const max = avg.map((v) => v + 4);
  return {
    t,
    series: [{ name: "cpu.util", avg, min, max, min_max_source: "avg_of_60s" }],
    downsampled: true,
    bucket_s: 60,
  };
}

function ok(route: Route, body: unknown) {
  return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}

test.describe("Device telemetry UI", () => {
  test.beforeEach(async ({ authedPage }) => {
    await authedPage.route(`**/api/v1/devices/${DEVICE_ID}`, (route: Route) => ok(route, fakeDevice()));
    await authedPage.route("**/api/v1/sites", (route: Route) =>
      ok(route, [{ id: GROUP_ID, name: "default", created_at: "", updated_at: "" }]),
    );
    await authedPage.route(`**/api/v1/sessions?device_id=${DEVICE_ID}*`, (route: Route) => ok(route, []));
    await authedPage.route("**/api/v1/amt/devices", (route: Route) => ok(route, []));
    await authedPage.route("**/api/v1/updates/manifests*", (route: Route) => ok(route, []));
    await authedPage.route(`**/api/v1/devices/${DEVICE_ID}/hardware`, (route: Route) => route.fulfill({ status: 404, body: "" }));
    await authedPage.route(`**/api/v1/devices/${DEVICE_ID}/metrics*`, (route: Route) => ok(route, fakeMetrics()));
  });

  test("renders the anomaly panel and a metric timeline", async ({ authedPage }) => {
    await authedPage.goto(`/devices/${DEVICE_ID}`);

    await expect(authedPage.getByRole("heading", { name: "Telemetry", exact: true })).toBeVisible();
    await expect(authedPage.getByText("80%")).toBeVisible();
    await expect(authedPage.getByRole("figure", { name: "cpu metrics" })).toBeVisible();
  });

  test("correlation jump: 'view logs for this window' fetches that window", async ({ authedPage }) => {
    let logsQuery: URLSearchParams | null = null;
    await authedPage.route(`**/api/v1/devices/${DEVICE_ID}/logs*`, (route: Route) => {
      logsQuery = new URL(route.request().url()).searchParams;
      return ok(route, {
        entries: [{ timestamp: new Date().toISOString(), level: "ERROR", target: "mesh_agent::core", message: "window-scoped log line" }],
        total: 1,
        has_more: false,
      });
    });

    await authedPage.goto(`/devices/${DEVICE_ID}`);
    await authedPage.getByRole("button", { name: /view logs for this window/i }).click();

    await expect(authedPage.getByText("window-scoped log line")).toBeVisible();
    expect(logsQuery).not.toBeNull();
    expect(logsQuery!.get("from")).toBeTruthy();
    expect(logsQuery!.get("to")).toBeTruthy();
  });
});
