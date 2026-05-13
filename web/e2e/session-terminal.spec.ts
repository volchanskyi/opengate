import { test, expect } from "./fixtures";
import type { Route, WebSocketRoute } from "@playwright/test";

// Phase B / B1: Session + terminal E2E.
//
// The docker-compose.test.yml stack has no real agent — actual terminal byte
// flow (msgpack-framed over the relay WebSocket) is covered by:
//   - web/src/features/terminal/TerminalView.test.tsx (unit, xterm wiring)
//   - testdata/golden (Rust↔Go wire-protocol fidelity)
//
// This spec exercises the core *session-flow* loop in the browser:
//   1. Start Session on an online device → SessionView opens
//   2. Terminal tab is the default and the container renders
//   3. The relay WebSocket is opened with the issued token
//   4. Disconnect navigates back to /devices
//   5. Offline device leaves the Start Session button disabled
//   6. Session creation failure surfaces an error toast
//
// API responses are stubbed via page.route(); the relay WebSocket is
// intercepted via page.routeWebSocket() so the spec is hermetic.

const DEVICE_ID_ONLINE = "11111111-1111-4111-8111-aaaaaaaaaaaa";
const DEVICE_ID_OFFLINE = "11111111-1111-4111-8111-bbbbbbbbbbbb";
const GROUP_ID = "33333333-3333-4333-8333-333333333333";
const SESSION_TOKEN = "e2e-session-token-00000000000000000000000000";
const RELAY_URL = "wss://relay.invalid/relay";

function fakeDevice(id: string, status: "online" | "offline") {
  const now = new Date().toISOString();
  return {
    id,
    group_id: GROUP_ID,
    hostname: `e2e-session-${status}`,
    os: "linux",
    os_display: "Linux",
    agent_version: "0.1.0",
    capabilities: ["Terminal", "FileManager"],
    status,
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

type AuthedPage = Parameters<Parameters<typeof test>[2]>[0]["authedPage"];

async function stubCommonRoutes(page: AuthedPage, id: string, status: "online" | "offline") {
  await page.route(`**/api/v1/devices/${id}`, (route: Route) =>
    ok(route, fakeDevice(id, status)),
  );
  await page.route("**/api/v1/groups", (route: Route) =>
    ok(route, [
      { id: GROUP_ID, name: "default", owner_id: "x", created_at: "", updated_at: "" },
    ]),
  );
  await page.route(`**/api/v1/sessions?device_id=${id}*`, (route: Route) =>
    ok(route, []),
  );
  await page.route("**/api/v1/amt/devices", (route: Route) => ok(route, []));
  await page.route("**/api/v1/updates/manifests*", (route: Route) => ok(route, []));
  await page.route(`**/api/v1/devices/${id}/hardware`, (route: Route) =>
    route.fulfill({ status: 404, body: "" }),
  );
}

async function stubSessionCreate(page: AuthedPage) {
  await page.route("**/api/v1/sessions", (route: Route) => {
    if (route.request().method() !== "POST") return route.fallback();
    return ok(route, { token: SESSION_TOKEN, relay_url: RELAY_URL });
  });
}

test.describe("Session terminal flow", () => {
  test("Start Session on online device opens SessionView with Terminal active", async ({
    authedPage,
  }) => {
    await stubCommonRoutes(authedPage, DEVICE_ID_ONLINE, "online");
    await stubSessionCreate(authedPage);
    // Swallow the relay WebSocket — never deliver frames; the connection
    // overlay will remain visible because the transport never reaches
    // 'connected'. That's fine: this test asserts the *flow*, not state.
    await authedPage.routeWebSocket(RELAY_URL, () => {
      /* no-op: leave the socket open but silent */
    });

    await authedPage.goto(`/devices/${DEVICE_ID_ONLINE}`);
    await authedPage.getByRole("button", { name: /start session/i }).click();

    await expect(authedPage).toHaveURL(new RegExp(`/sessions/${SESSION_TOKEN}$`));
    await expect(authedPage.getByRole("tablist")).toBeVisible();

    // Terminal is the default active tab when capabilities omit RemoteDesktop.
    const terminalTab = authedPage.getByRole("tab", { name: "Terminal" });
    await expect(terminalTab).toBeVisible();
    await expect(terminalTab).toHaveAttribute("aria-selected", "true");

    await expect(authedPage.locator('[data-testid="terminal-container"]')).toBeVisible();
  });

  test("relay WebSocket is opened with side=browser and an auth token", async ({
    authedPage,
  }) => {
    await stubCommonRoutes(authedPage, DEVICE_ID_ONLINE, "online");
    await stubSessionCreate(authedPage);

    let observedUrl: string | null = null;
    await authedPage.routeWebSocket(
      (url: URL) => url.href.startsWith(RELAY_URL),
      (ws: WebSocketRoute) => {
        observedUrl = ws.url();
      },
    );

    await authedPage.goto(`/devices/${DEVICE_ID_ONLINE}`);
    await authedPage.getByRole("button", { name: /start session/i }).click();
    await expect(authedPage).toHaveURL(new RegExp(`/sessions/${SESSION_TOKEN}$`));

    // WSTransport appends side=browser and a non-empty auth JWT query param.
    await expect.poll(() => observedUrl).not.toBeNull();
    expect(observedUrl).toMatch(/\bside=browser\b/);
    expect(observedUrl).toMatch(/\bauth=[^&]+/);
  });

  test("Disconnect button returns to /devices", async ({ authedPage }) => {
    await stubCommonRoutes(authedPage, DEVICE_ID_ONLINE, "online");
    await stubSessionCreate(authedPage);
    await authedPage.routeWebSocket(RELAY_URL, () => {
      /* no-op */
    });

    await authedPage.goto(`/devices/${DEVICE_ID_ONLINE}`);
    await authedPage.getByRole("button", { name: /start session/i }).click();
    await expect(authedPage).toHaveURL(new RegExp(`/sessions/${SESSION_TOKEN}$`));

    await authedPage.getByRole("button", { name: /disconnect/i }).click();
    await expect(authedPage).toHaveURL(/\/devices$/);
  });

  test("offline device shows error toast and stays on device page", async ({
    authedPage,
  }) => {
    await stubCommonRoutes(authedPage, DEVICE_ID_OFFLINE, "offline");
    // Start Session is not client-side-gated on device.status; the server
    // returns an error when the agent isn't reachable.
    await authedPage.route("**/api/v1/sessions", (route: Route) => {
      if (route.request().method() !== "POST") return route.fallback();
      return route.fulfill({
        status: 502,
        contentType: "application/json",
        body: JSON.stringify({ error: "agent unreachable" }),
      });
    });

    await authedPage.goto(`/devices/${DEVICE_ID_OFFLINE}`);
    await authedPage.getByRole("button", { name: /start session/i }).click();

    await expect(
      authedPage.getByText(/failed to start session/i),
    ).toBeVisible();
    await expect(authedPage).toHaveURL(new RegExp(`/devices/${DEVICE_ID_OFFLINE}$`));
  });
});
