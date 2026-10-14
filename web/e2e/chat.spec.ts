/// <reference types="node" />
import { test, expect } from "./fixtures";
import type { Route, WebSocketRoute } from "@playwright/test";
import { decode, encode } from "@msgpack/msgpack";
import { mockRelayUrl, openSessionTab, stubSessionStart } from "./helpers/session-tab";

// The Chat tab needs RemoteDesktop, which a Linux agent lacks, so the device, the session and the
// relay are stubbed on the context, which the session tab inherits.

const DEVICE_ID = "11111111-1111-4111-8111-dddddddddddd";
const GROUP_ID = "33333333-3333-4333-8333-333333333333";

// Control frame type byte, as in web/src/lib/protocol/types.ts.
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
    site_id: GROUP_ID,
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
  const context = page.context();
  await stubSessionStart(context, test.info().project.use.baseURL!);
  await context.route(`**/api/v1/devices/${DEVICE_ID}`, (route: Route) => ok(route, fakeDevice()));
  await context.route("**/api/v1/sites", (route: Route) =>
    ok(route, [{ id: GROUP_ID, name: "default", created_at: "", updated_at: "" }]),
  );
  await context.route(`**/api/v1/sessions?device_id=${DEVICE_ID}*`, (route: Route) => ok(route, []));
  await context.route("**/api/v1/amt/devices", (route: Route) => ok(route, []));
  await context.route("**/api/v1/updates/manifests*", (route: Route) => ok(route, []));
  await context.route(`**/api/v1/devices/${DEVICE_ID}/hardware`, (route: Route) =>
    route.fulfill({ status: 404, body: "" }),
  );
}

async function mockRelayEcho(page: AuthedPage): Promise<{ sent: string[] }> {
  const sent: string[] = [];
  const relayUrl = mockRelayUrl(test.info().project.use.baseURL!);
  await page.context().routeWebSocket(
    (url: URL) => url.href.startsWith(relayUrl),
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
  const tab = await openSessionTab(page, DEVICE_ID);
  await tab.getByRole("tab", { name: "Chat" }).click();
  return tab;
}

test.describe("Chat flow", () => {
  test("sending a message renders it and the agent echo", async ({ authedPage }) => {
    await stubCommonRoutes(authedPage);
    const relay = await mockRelayEcho(authedPage);

    const tab = await openChatTab(authedPage);

    const box = tab.getByPlaceholder("Type a message...");
    await box.fill("hello agent");
    await tab.getByRole("button", { name: "Send" }).click();

    // exact: the echo bubble also contains this substring.
    await expect(tab.getByText("hello agent", { exact: true })).toBeVisible();
    await expect(tab.getByText("echo: hello agent")).toBeVisible();
    expect(relay.sent).toContain("hello agent");
    await expect(box).toHaveValue("");
  });

  test("Enter key sends the message", async ({ authedPage }) => {
    await stubCommonRoutes(authedPage);
    await mockRelayEcho(authedPage);

    const tab = await openChatTab(authedPage);

    const box = tab.getByPlaceholder("Type a message...");
    await box.fill("via enter");
    await box.press("Enter");

    await expect(tab.getByText("via enter", { exact: true })).toBeVisible();
    await expect(tab.getByText("echo: via enter")).toBeVisible();
  });
});
