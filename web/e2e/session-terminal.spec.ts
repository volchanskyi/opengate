import { test, expect } from "./fixtures";
import type { Route } from "@playwright/test";
import { enrolledMachine, MACHINE_A } from "./helpers/enrolled-machine";

// Session + terminal flow in the browser, against a machine that is actually
// running.
//
// The stack carries two enrolled agents, so "a technician opens a terminal on
// the customer's machine" is a real session: the server mints a real token
// against a real online machine and tells the browser a real relay address, and
// the browser connects to it. What is asserted is the flow a technician sees —
// the workspace opens, Terminal is the tab it opens on, the relay connection is
// made as the browser side of that session, and disconnecting comes back to the
// fleet.
//
// The bytes that then travel are covered by
// web/src/features/terminal/TerminalView.test.tsx and by the cross-language
// golden fixtures, which can pin them because they supply them.

type AuthedPage = Parameters<Parameters<typeof test>[2]>[0]["authedPage"];

// isRelay matches the relay socket the browser opens for a session, whatever
// address the server handed out.
function isRelay(url: URL): boolean {
  return url.pathname.includes("/relay");
}

// startSession opens a session on the given machine and returns once the
// workspace is showing.
async function startSession(page: AuthedPage, deviceID: string) {
  await page.goto(`/devices/${deviceID}`);
  await page.getByRole("button", { name: /start session/i }).click();
  await expect(page).toHaveURL(/\/sessions\/[^/]+$/);
}

test.describe("Session terminal flow", () => {
  test("Start Session on an online machine opens the workspace with Terminal active", async ({
    authedPage,
    request,
  }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    await startSession(authedPage, machine.id);

    await expect(authedPage.getByRole("tablist")).toBeVisible();

    // Terminal is the default active tab when capabilities omit RemoteDesktop,
    // which is what a Linux machine reports.
    const terminalTab = authedPage.getByRole("tab", { name: "Terminal" });
    await expect(terminalTab).toBeVisible();
    await expect(terminalTab).toHaveAttribute("aria-selected", "true");

    await expect(authedPage.locator('[data-testid="terminal-container"]')).toBeVisible();
  });

  test("the relay connection is made as the browser side of the session", async ({
    authedPage,
    request,
  }) => {
    const machine = await enrolledMachine(request, MACHINE_A);

    let observedUrl: string | null = null;
    authedPage.on("websocket", (ws) => {
      if (isRelay(new URL(ws.url()))) observedUrl = ws.url();
    });

    await startSession(authedPage, machine.id);

    // WSTransport appends side=browser and a non-empty auth JWT query param.
    await expect.poll(() => observedUrl).not.toBeNull();
    expect(observedUrl).toMatch(/\bside=browser\b/);
    expect(observedUrl).toMatch(/\bauth=[^&]+/);
  });

  test("Disconnect returns to the fleet", async ({ authedPage, request }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    await startSession(authedPage, machine.id);

    await authedPage.getByRole("button", { name: /disconnect/i }).click();
    await expect(authedPage).toHaveURL(/\/devices$/);
  });

  test("a session the server refuses surfaces an error toast", async ({
    authedPage,
    request,
  }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    // The refusal a technician meets when the machine drops off between the
    // page loading and the click. Producing it on demand would mean taking a
    // machine off the network mid-run, so the answer is supplied; the machine
    // and the page are real.
    await authedPage.route("**/api/v1/sessions", (route: Route) => {
      if (route.request().method() !== "POST") return route.fallback();
      return route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({ error: "agent not connected" }),
      });
    });

    await authedPage.goto(`/devices/${machine.id}`);
    await authedPage.getByRole("button", { name: /start session/i }).click();

    await expect(authedPage.getByText(/failed to start session/i)).toBeVisible();
    await expect(authedPage).toHaveURL(new RegExp(`/devices/${machine.id}$`));
  });
});
