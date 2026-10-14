/// <reference types="node" />
import { test, expect } from "./fixtures";
import type { Page, WebSocket } from "@playwright/test";
import { ATTEMPT_BUDGET_MS } from "../src/lib/direct/handoff";
import { enrolledMachine, MACHINE_A, MACHINE_B } from "./helpers/enrolled-machine";
import { decodeControlFrame } from "./helpers/frames";
import { endHostSession, msUntilPath, noteRelayOpen, openSessionTab, sessionPath } from "./helpers/session-tab";
import { onMachine, outcomeCounts, resetRouters, router } from "./helpers/stack";

// The browser runs on the test machine, beside agent-b on the stack's internet network; agent-a
// sits behind the office router, whose rules each spec changes and puts back.

const LINK_CONTROL = new Set(["DirectOffer", "DirectAnswer", "IceCandidate", "DirectRefused", "DirectSwitch", "DirectFallback"]);

// The browser's attempt timer fires on its own event loop, a few milliseconds after it is due.
const TIMER_SLACK_MS = 500;

// The silence limit is 3 s, counted from the last packet; step 1 measured 2.2–2.9 s, once 4.7 s.
const FALLBACK_LIMIT_MS = 6_000;

/** Every frame the session tab's relay socket carries, with the time it was seen. */
function recordRelay(page: Page): { at: number; data: Buffer }[] {
  const frames: { at: number; data: Buffer }[] = [];
  page.context().on("page", (tab: Page) =>
    tab.on("websocket", (ws: WebSocket) => {
      if (!new URL(ws.url()).pathname.includes("/relay")) return;
      const keep = (frame: { payload: string | Buffer }) => {
        if (typeof frame.payload !== "string") frames.push({ at: Date.now(), data: frame.payload });
      };
      ws.on("framesent", keep);
      ws.on("framereceived", keep);
    }),
  );
  return frames;
}

/** What a relay frame is: a link-control message's name, or the kind of session data it carries. */
function relayKind(data: Buffer): string {
  const message = decodeControlFrame(data);
  if (!message) return `frame type ${data[0]}`;
  return LINK_CONTROL.has(message.type) ? "link-control" : message.type;
}

async function terminalEchoes(tab: Page, tag: string): Promise<void> {
  await tab.locator('[data-testid="terminal-container"]').click();
  await tab.keyboard.type(`echo opengate-$((6*7))-${tag}\n`);
  await expect(tab.locator(".xterm-rows")).toContainText(`opengate-42-${tag}`, { timeout: 15_000 });
}

/** The machine's UDP sockets, read from its own network namespace. */
function udpSockets(machine: string): number {
  return Number(onMachine(machine, "cat /proc/net/udp /proc/net/udp6 | grep -vc local_address").trim());
}

test.describe("Direct link", () => {
  test.afterEach(async ({ request }) => {
    resetRouters();
    for (const name of [MACHINE_A, MACHINE_B]) {
      await endHostSession(request, (await enrolledMachine(request, name)).id);
    }
  });

  for (const name of [MACHINE_B, MACHINE_A]) {
    test(`${name} reaches Direct, and the relay carries no session data once it has`, async ({
      authedPage,
      request,
    }) => {
      const machine = await enrolledMachine(request, name);
      const frames = recordRelay(authedPage);
      const before = await outcomeCounts();

      const tab = await openSessionTab(authedPage, machine.id);
      await expect(sessionPath(tab)).toHaveText("Direct", { timeout: ATTEMPT_BUDGET_MS });
      await tab.waitForTimeout(500);
      const switched = Date.now();

      await terminalEchoes(tab, "direct");
      await tab.getByRole("tab", { name: "Files" }).click();
      await expect(tab.getByRole("button", { name: "tmp", exact: true })).toBeVisible();

      const after = frames.filter((f) => f.at > switched).map((f) => relayKind(f.data));
      expect(after.filter((kind) => kind !== "link-control")).toEqual([]);
      expect((await outcomeCounts()).direct).toBe(before.direct! + 1);
    });
  }

  test("UDP allowed only to the helper leaves the session Through server, inside the attempt budget", async ({
    authedPage,
    request,
  }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    router("office", "udp", "helper-only");
    const before = await outcomeCounts();

    await noteRelayOpen(authedPage.context());
    const tab = await openSessionTab(authedPage, machine.id);
    const fellBack = await msUntilPath(tab, "Through server: no direct route on this network", ATTEMPT_BUDGET_MS + TIMER_SLACK_MS);
    expect(fellBack).toBeLessThanOrEqual(ATTEMPT_BUDGET_MS + TIMER_SLACK_MS);

    await terminalEchoes(tab, "relayed");
    await expect.poll(async () => (await outcomeCounts()).no_route).toBe(before.no_route! + 1);
  });

  test("a 1 s drop keeps the session Direct; a 5 s drop moves it to the relay once, keystrokes intact", async ({
    authedPage,
    request,
  }) => {
    test.setTimeout(90_000);
    const machine = await enrolledMachine(request, MACHINE_A);
    const before = await outcomeCounts();
    const tab = await openSessionTab(authedPage, machine.id);
    await expect(sessionPath(tab)).toHaveText("Direct", { timeout: ATTEMPT_BUDGET_MS });
    await tab.locator('[data-testid="terminal-container"]').click();

    // Keystrokes typed into a dead link arrive late and in order once it answers again.
    router("office", "udp", "helper-only");
    await tab.keyboard.type("echo opengate-");
    await tab.waitForTimeout(1_000);
    router("office", "udp", "open");
    await tab.keyboard.type("$((6*7))-short-drop\n");
    await expect(tab.locator(".xterm-rows")).toContainText("opengate-42-short-drop", { timeout: 15_000 });
    await tab.waitForTimeout(5_000);
    await expect(sessionPath(tab)).toHaveText("Direct");

    const cut = Date.now();
    router("office", "udp", "helper-only");
    await tab.keyboard.type("echo opengate-");
    await expect(sessionPath(tab)).toHaveText("Through server", { timeout: FALLBACK_LIMIT_MS });
    expect(Date.now() - cut).toBeLessThan(FALLBACK_LIMIT_MS);
    await tab.keyboard.type("$((6*7))-long-drop\n");
    await expect(tab.locator(".xterm-rows")).toContainText("opengate-42-long-drop", { timeout: 15_000 });

    // A session changes path at most twice, so the restored route leaves it on the relay.
    await tab.waitForTimeout(Math.max(0, 5_000 - (Date.now() - cut)));
    router("office", "udp", "open");
    await tab.waitForTimeout(5_000);
    await expect(sessionPath(tab)).toHaveText("Through server");
    await expect.poll(async () => (await outcomeCounts()).dropped).toBe(before.dropped! + 1);
  });

  test("ending the session through the API while Direct ends both sides and closes the host's link", async ({
    authedPage,
    request,
  }) => {
    const machine = await enrolledMachine(request, MACHINE_B);
    const idle = udpSockets(MACHINE_B);
    const tab = await openSessionTab(authedPage, machine.id);
    await expect(sessionPath(tab)).toHaveText("Direct", { timeout: ATTEMPT_BUDGET_MS });
    expect(udpSockets(MACHINE_B)).toBeGreaterThan(idle);

    const ended = Date.now();
    await endHostSession(request, machine.id);

    await expect(tab.getByText(/^Ended by /)).toBeVisible({ timeout: 5_000 });
    await expect.poll(() => udpSockets(MACHINE_B), { timeout: 5_000 }).toBe(idle);
    expect(Date.now() - ended).toBeLessThan(5_000);
  });
});
