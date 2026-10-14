import { test, expect } from "./fixtures";
import type { Page } from "@playwright/test";
import { ATTEMPT_BUDGET_MS } from "../src/lib/direct/handoff";
import { enrolledMachine, MACHINE_A } from "./helpers/enrolled-machine";
import { endHostSession, msUntilPath, noteRelayOpen, openSessionTab, sessionPath } from "./helpers/session-tab";
import { resetRouters, router } from "./helpers/stack";

// The browser sits behind the home router and agent-a behind the office router, as a technician
// at home meets a customer's office; this file runs in that browser's project alone.

const NO_ROUTE = "Through server: no direct route on this network";
// The browser's attempt timer fires on its own event loop, a few milliseconds after it is due.
const TIMER_SLACK_MS = 500;

async function echoes(tab: Page, tag: string): Promise<void> {
  await tab.locator('[data-testid="terminal-container"]').click();
  await tab.keyboard.type(`echo opengate-$((6*7))-${tag}\n`);
  await expect(tab.locator(".xterm-rows")).toContainText(`opengate-42-${tag}`, { timeout: 15_000 });
}

test.describe("Both sides behind routers", () => {
  test.afterEach(async ({ request }) => {
    resetRouters();
    await endHostSession(request, (await enrolledMachine(request, MACHINE_A)).id);
  });

  test("routers that keep each socket's port give Direct", async ({ authedPage, request }) => {
    const tab = await openSessionTab(authedPage, (await enrolledMachine(request, MACHINE_A)).id);
    await expect(sessionPath(tab)).toHaveText("Direct", { timeout: ATTEMPT_BUDGET_MS });
    await echoes(tab, "two-routers");
  });

  for (const shape of [
    { name: "UDP blocked at the home router", apply: () => router("home", "udp", "blocked") },
    {
      name: "both routers scrambling ports",
      apply: () => {
        router("office", "nat", "scramble");
        router("home", "nat", "scramble");
      },
    },
  ]) {
    test(`${shape.name} leaves the session Through server, inside the attempt budget`, async ({
      authedPage,
      request,
    }) => {
      shape.apply();
      await noteRelayOpen(authedPage.context());
      const tab = await openSessionTab(authedPage, (await enrolledMachine(request, MACHINE_A)).id);
      const fellBack = await msUntilPath(tab, NO_ROUTE, ATTEMPT_BUDGET_MS + TIMER_SLACK_MS);
      expect(fellBack).toBeLessThanOrEqual(ATTEMPT_BUDGET_MS + TIMER_SLACK_MS);
      await echoes(tab, "relayed");
    });
  }
});
