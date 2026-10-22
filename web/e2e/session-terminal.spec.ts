import { test, expect } from "./fixtures";
import type { Page, Route, WebSocket } from "@playwright/test";
import { enrolledMachine, MACHINE_A } from "./helpers/enrolled-machine";
import { endHostSession, openSessionTab } from "./helpers/session-tab";

// Sessions are real: the server opens each against an enrolled machine that is online, and the
// machine dials the relay address the server hands it.

// Matches the relay socket whatever address the server hands out.
function isRelay(url: URL): boolean {
  return url.pathname.includes("/relay");
}

test.describe("Session terminal flow", () => {
  test.afterEach(async ({ request }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    await endHostSession(request, machine.id);
  });

  test("Open session opens the host's tab with Terminal active, and the machine echoes", async ({
    authedPage,
    request,
  }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    const tab = await openSessionTab(authedPage, machine.id);

    await expect(tab.getByRole("tablist")).toBeVisible();
    await expect(tab.getByRole("tab", { name: "Terminal" })).toHaveAttribute("aria-selected", "true");
    await expect(tab).toHaveTitle(new RegExp(`^${MACHINE_A} — `));

    // The output differs from the typed line, so only the machine's shell can produce it.
    await tab.locator('[data-testid="terminal-container"]').click();
    await tab.keyboard.type("echo opengate-$((6*7))-echoed\n");
    await expect(tab.locator(".xterm-rows")).toContainText("opengate-42-echoed", { timeout: 15_000 });
  });

  test("the relay connection is the browser side, signed in by its cookie alone", async ({
    authedPage,
    request,
  }) => {
    const machine = await enrolledMachine(request, MACHINE_A);

    let observedUrl: string | null = null;
    authedPage.context().on("page", (page: Page) =>
      page.on("websocket", (ws: WebSocket) => {
        if (isRelay(new URL(ws.url()))) observedUrl = ws.url();
      }),
    );

    await openSessionTab(authedPage, machine.id);

    await expect.poll(() => observedUrl).not.toBeNull();
    expect(observedUrl).toMatch(/\bside=browser\b/);
    expect(observedUrl).not.toMatch(/\bauth=/);
  });

  test("a session outlives the server's keep-alive with no connection error", async ({
    authedPage,
    request,
  }) => {
    test.setTimeout(90_000);
    const machine = await enrolledMachine(request, MACHINE_A);

    let relayClosed = false;
    authedPage.context().on("page", (page: Page) =>
      page.on("websocket", (ws: WebSocket) => {
        if (isRelay(new URL(ws.url()))) ws.on("close", () => { relayClosed = true; });
      }),
    );

    const tab = await openSessionTab(authedPage, machine.id);
    await expect(tab.locator('[data-testid="terminal-container"]')).toBeVisible();

    // The server pings each side of the relay every 20 seconds, so one ping passes here.
    await tab.waitForTimeout(25_000);

    await expect(tab.getByText(/unknown frame type/i)).toHaveCount(0);
    await expect(tab.locator('[data-testid="terminal-container"]')).toBeVisible();
    expect(relayClosed).toBe(false);
  });

  test("Disconnect ends the session and closes its tab", async ({ authedPage, request }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    const tab = await openSessionTab(authedPage, machine.id);
    await expect(tab.locator('[data-testid="terminal-container"]')).toBeVisible();

    const closed = tab.waitForEvent("close");
    // Firefox closes the tab before the click returns, and the click reports that as an error.
    await tab
      .getByRole("button", { name: /disconnect/i })
      .click()
      .catch((error: unknown) => {
        if (!tab.isClosed()) throw error;
      });
    await closed;

    await authedPage.reload();
    await expect(authedPage.getByRole("button", { name: "Open session" })).toBeEnabled();
  });

  test("a session the server refuses shows why in the tab", async ({ authedPage, request }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    // The refusal a machine dropping off between page load and click produces.
    await authedPage.context().route("**/api/v1/sessions", (route: Route) => {
      if (route.request().method() !== "POST") return route.fallback();
      return route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({ error: "agent not connected" }),
      });
    });

    const tab = await openSessionTab(authedPage, machine.id);

    await expect(tab.getByText("agent not connected")).toBeVisible();
    await expect(tab.getByRole("tablist")).toHaveCount(0);
  });
});
