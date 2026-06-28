/// <reference types="node" />
import { test, expect } from "./fixtures";
import type { Route, WebSocketRoute } from "@playwright/test";
import { decode, encode } from "@msgpack/msgpack";

// Chat (MessengerView) end-to-end send→echo.
//
// MessengerView sends a ChatMessage control frame over the relay WebSocket and
// renders incoming ChatMessage frames. Like file-manager.spec.ts, this mocks
// the relay at the wire level: routeWebSocket decodes the browser's outbound
// ChatMessage frame and echoes a reply frame back, so the full encode → relay →
// decode → store → render path runs against real msgpack bytes. The component's
// rendering details are unit-tested in MessengerView.test.tsx; this covers the
// route-level integration the unit test cannot reach.

const DEVICE_ID = "11111111-1111-4111-8111-dddddddddddd";
const GROUP_ID = "33333333-3333-4333-8333-333333333333";
const SESSION_TOKEN = "e2e-chat-token-00000000000000000000000000000000";
const RELAY_URL = "wss://relay.invalid/relay";

// Mirrors web/src/lib/protocol/types.ts
const FRAME_CONTROL = 0x01;

function encodeControlFrame(message: object): Buffer {
  const payload = encode(message);
  const buf = Buffer.alloc(5 + payload.length);
  buf[0] = FRAME_CONTROL;
  buf.writeUInt32BE(payload.length, 1);
  buf.set(payload, 5);
  return buf;
}

function decodeControlFrame(data: Buffer): { type: string; [key: string]: unknown } | null {
  if (data.length < 5 || data[0] !== FRAME_CONTROL) return null;
  const len = data.readUInt32BE(1);
  return decode(data.subarray(5, 5 + len)) as { type: string; [key: string]: unknown };
}

function ok(route: Route, body: unknown) {
  return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}

function fakeDevice() {
  const now = new Date().toISOString();
  return {
    id: DEVICE_ID,
    group_id: GROUP_ID,
    hostname: "e2e-chat-host",
    os: "linux",
    os_display: "Linux",
    agent_version: "0.1.0",
    capabilities: ["RemoteDesktop", "Terminal"],
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
    ok(route, [{ id: GROUP_ID, name: "default", owner_id: "x", created_at: "", updated_at: "" }]),
  );
  await page.route(`**/api/v1/sessions?device_id=${DEVICE_ID}*`, (route: Route) => ok(route, []));
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

// mockRelayEcho replies to each outbound browser ChatMessage with an
// agent-sent ChatMessage echoing the text, and records what the browser sent.
async function mockRelayEcho(page: AuthedPage): Promise<{ sent: string[] }> {
  const sent: string[] = [];
  await page.routeWebSocket(
    (url: URL) => url.href.startsWith(RELAY_URL),
    (ws: WebSocketRoute) => {
      ws.onMessage((raw) => {
        if (typeof raw === "string") return;
        const msg = decodeControlFrame(raw);
        if (!msg || msg.type !== "ChatMessage" || msg.sender !== "browser") return;
        const text = msg.text as string;
        sent.push(text);
        ws.send(encodeControlFrame({ type: "ChatMessage", text: `echo: ${text}`, sender: "agent" }));
      });
    },
  );
  return { sent };
}

async function openChatTab(page: AuthedPage) {
  await page.goto(`/devices/${DEVICE_ID}`);
  await page.getByRole("button", { name: /start session/i }).click();
  await expect(page).toHaveURL(new RegExp(`/sessions/${SESSION_TOKEN}$`));
  await page.getByRole("tab", { name: "Chat" }).click();
}

test.describe("Chat flow", () => {
  test("sending a message renders it and the agent echo", async ({ authedPage }) => {
    await stubCommonRoutes(authedPage);
    const relay = await mockRelayEcho(authedPage);

    await openChatTab(authedPage);

    const box = authedPage.getByPlaceholder("Type a message...");
    await box.fill("hello agent");
    await authedPage.getByRole("button", { name: "Send" }).click();

    // Optimistic local render of the sent message (exact: the echo bubble
    // below also contains this substring).
    await expect(authedPage.getByText("hello agent", { exact: true })).toBeVisible();
    // Echo travelled the full wire path back and rendered.
    await expect(authedPage.getByText("echo: hello agent")).toBeVisible();
    expect(relay.sent).toContain("hello agent");
    // Input clears after send.
    await expect(box).toHaveValue("");
  });

  test("Enter key sends the message", async ({ authedPage }) => {
    await stubCommonRoutes(authedPage);
    await mockRelayEcho(authedPage);

    await openChatTab(authedPage);

    const box = authedPage.getByPlaceholder("Type a message...");
    await box.fill("via enter");
    await box.press("Enter");

    await expect(authedPage.getByText("via enter", { exact: true })).toBeVisible();
    await expect(authedPage.getByText("echo: via enter")).toBeVisible();
  });
});
