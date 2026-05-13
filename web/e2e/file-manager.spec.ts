/// <reference types="node" />
import { test, expect } from "./fixtures";
import type { Route, WebSocketRoute } from "@playwright/test";
import { decode, encode } from "@msgpack/msgpack";

// Phase B / B2: File Manager E2E.
//
// The FileManagerView reads from a Zustand store fed by msgpack-encoded
// control messages over the relay WebSocket. This spec mocks the relay
// at the wire level: routeWebSocket intercepts the browser's connection,
// the test handler decodes the FileListRequest frames the browser sends,
// and pushes back FileListResponse / FileListError frames. The full
// browser-side decode path (codec → connection-store → file-store →
// FileManagerView render) runs against real bytes — there is no
// component-level shortcut.
//
// FileManagerView component behavior (entry rendering, button states,
// breadcrumb, viewer) is also covered by the unit suite at
// web/src/features/file-manager/FileManagerView.test.tsx. This spec
// covers the route-level integration the unit test cannot reach: opening
// a session, switching to the Files tab, and exercising the wire path.

const DEVICE_ID = "11111111-1111-4111-8111-cccccccccccc";
const GROUP_ID = "33333333-3333-4333-8333-333333333333";
const SESSION_TOKEN = "e2e-fm-token-00000000000000000000000000000000";
const RELAY_URL = "wss://relay.invalid/relay";

// Mirrors web/src/lib/protocol/types.ts
const FRAME_CONTROL = 0x01;

interface FileEntry {
  name: string;
  is_dir: boolean;
  size: number;
  modified: number;
}

/** Encode a ControlMessage into a wire frame: [type=0x01][4-byte BE length][msgpack]. */
function encodeControlFrame(message: object): Buffer {
  const payload = encode(message);
  const buf = Buffer.alloc(5 + payload.length);
  buf[0] = FRAME_CONTROL;
  buf.writeUInt32BE(payload.length, 1);
  Buffer.from(payload).copy(buf, 5);
  return buf;
}

/** Decode a wire-format control frame; returns the inner ControlMessage. */
function decodeControlFrame(data: Buffer): { type: string; [key: string]: unknown } | null {
  if (data.length < 5 || data[0] !== FRAME_CONTROL) return null;
  const len = data.readUInt32BE(1);
  const payload = data.subarray(5, 5 + len);
  return decode(payload) as { type: string; [key: string]: unknown };
}

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
    hostname: "e2e-fm-host",
    os: "linux",
    os_display: "Linux",
    agent_version: "0.1.0",
    capabilities: ["Terminal", "FileManager"],
    status: "online",
    last_seen: now,
    created_at: now,
    updated_at: now,
  };
}

type AuthedPage = Parameters<Parameters<typeof test>[2]>[0]["authedPage"];

async function stubCommonRoutes(page: AuthedPage) {
  await page.route(`**/api/v1/devices/${DEVICE_ID}`, (route: Route) => ok(route, fakeDevice()));
  await page.route("**/api/v1/groups", (route: Route) =>
    ok(route, [
      { id: GROUP_ID, name: "default", owner_id: "x", created_at: "", updated_at: "" },
    ]),
  );
  await page.route(`**/api/v1/sessions?device_id=${DEVICE_ID}*`, (route: Route) =>
    ok(route, []),
  );
  await page.route("**/api/v1/amt/devices", (route: Route) => ok(route, []));
  await page.route("**/api/v1/updates/manifests*", (route: Route) => ok(route, []));
  await page.route(`**/api/v1/devices/${DEVICE_ID}/hardware`, (route: Route) =>
    route.fulfill({ status: 404, body: "" }),
  );
  await page.route("**/api/v1/sessions", (route: Route) => {
    if (route.request().method() !== "POST") return route.fallback();
    return ok(route, { token: SESSION_TOKEN, relay_url: RELAY_URL });
  });
}

/**
 * Install a relay handler that responds to FileListRequest frames with
 * caller-supplied entries (keyed by requested path). Tracks every
 * request the browser sent so tests can assert on the sequence.
 */
async function mockRelay(
  page: AuthedPage,
  listings: Record<string, FileEntry[]>,
  errors: Record<string, string> = {},
): Promise<{ requestedPaths: string[] }> {
  const requestedPaths: string[] = [];

  await page.routeWebSocket(
    (url: URL) => url.href.startsWith(RELAY_URL),
    (ws: WebSocketRoute) => {
      ws.onMessage((raw) => {
        if (typeof raw === "string") return; // Wire frames are binary.
        const msg = decodeControlFrame(raw);
        if (!msg || msg.type !== "FileListRequest") return;
        const path = msg.path as string;
        requestedPaths.push(path);

        if (path in errors) {
          ws.send(
            encodeControlFrame({ type: "FileListError", path, error: errors[path] }),
          );
          return;
        }
        const entries = listings[path] ?? [];
        ws.send(encodeControlFrame({ type: "FileListResponse", path, entries }));
      });
    },
  );

  return { requestedPaths };
}

async function openFilesTab(page: AuthedPage) {
  await page.goto(`/devices/${DEVICE_ID}`);
  await page.getByRole("button", { name: /start session/i }).click();
  await expect(page).toHaveURL(new RegExp(`/sessions/${SESSION_TOKEN}$`));
  await page.getByRole("tab", { name: "Files" }).click();
}

test.describe("File Manager flow", () => {
  test("Files tab renders the directory listing from a FileListResponse", async ({
    authedPage,
  }) => {
    await stubCommonRoutes(authedPage);
    const tracker = await mockRelay(authedPage, {
      "/": [
        { name: "docs", is_dir: true, size: 0, modified: 1_700_000_000 },
        { name: "README.md", is_dir: false, size: 1024, modified: 1_700_000_100 },
      ],
    });

    await openFilesTab(authedPage);

    await expect(authedPage.getByText("docs")).toBeVisible();
    await expect(authedPage.getByText("README.md")).toBeVisible();
    expect(tracker.requestedPaths).toContain("/");
  });

  test("clicking a directory navigates and loads the new listing", async ({
    authedPage,
  }) => {
    await stubCommonRoutes(authedPage);
    const tracker = await mockRelay(authedPage, {
      "/": [{ name: "docs", is_dir: true, size: 0, modified: 1_700_000_000 }],
      "/docs": [
        { name: "guide.txt", is_dir: false, size: 64, modified: 1_700_000_200 },
      ],
    });

    await openFilesTab(authedPage);

    await expect(authedPage.getByRole("button", { name: "docs" })).toBeVisible();
    await authedPage.getByRole("button", { name: "docs" }).click();

    await expect(authedPage.getByText("guide.txt")).toBeVisible();
    await expect(authedPage.getByText("/docs")).toBeVisible(); // breadcrumb path
    expect(tracker.requestedPaths).toEqual(["/", "/docs"]);

    // ".." button returns to root.
    await authedPage.getByRole("button", { name: ".." }).click();
    await expect(authedPage.getByRole("button", { name: "docs" })).toBeVisible();
    expect(tracker.requestedPaths).toEqual(["/", "/docs", "/"]);
  });

  test("permission-denied path renders an error banner", async ({ authedPage }) => {
    await stubCommonRoutes(authedPage);
    await mockRelay(
      authedPage,
      { "/": [{ name: "secret", is_dir: true, size: 0, modified: 1_700_000_000 }] },
      { "/secret": "permission denied" },
    );

    await openFilesTab(authedPage);
    await authedPage.getByRole("button", { name: "secret" }).click();

    await expect(authedPage.getByText(/permission denied/i)).toBeVisible();
  });
});
