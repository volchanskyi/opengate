import { expect, type APIRequestContext, type BrowserContext, type Locator, type Page } from "@playwright/test";
import { adminToken } from "./enrolled-machine";

/** Where a mocked relay answers: the server's own origin, which the page's policy allows. */
export function mockRelayUrl(baseURL: string): string {
  return new URL("/ws/relay/e2e-mock", baseURL).href.replace(/^http/, "ws");
}

/**
 * Answers the session tab's request for a session without the server, for specs that mock the
 * relay; the route is on the context, so the tab the device page opens inherits it.
 */
export async function stubSessionStart(context: BrowserContext, baseURL: string): Promise<void> {
  await context.route("**/api/v1/sessions", (route) => {
    if (route.request().method() !== "POST") return route.fallback();
    return route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({
        token: "e2e-mock-token-0000000000000000000000000000000",
        relay_url: mockRelayUrl(baseURL),
        ice_servers: [],
      }),
    });
  });
}

/** Opens the host's session tab from its device page, as a technician does, and returns the tab. */
export async function openSessionTab(page: Page, deviceId: string): Promise<Page> {
  await page.goto(`/devices/${deviceId}`);
  const [tab] = await Promise.all([
    page.context().waitForEvent("page"),
    page.getByRole("button", { name: "Open session" }).click(),
  ]);
  await expect(tab).toHaveURL(new RegExp(`/devices/${deviceId}/session$`));
  return tab;
}

/** The session bar's path: Direct or Through server. */
export function sessionPath(tab: Page): Locator {
  return tab.getByText(/^(Direct|Through server)$/);
}

/** Notes, in every page of the context, when its first WebSocket opens: a session tab's relay. */
export async function noteRelayOpen(context: BrowserContext): Promise<void> {
  await context.addInitScript(() => {
    const w = window as unknown as { relayOpenAt?: number };
    const Native = window.WebSocket;
    window.WebSocket = class extends Native {
      constructor(...args: ConstructorParameters<typeof WebSocket>) {
        super(...args);
        this.addEventListener("open", () => { w.relayOpenAt ??= performance.now(); });
      }
    };
  });
}

/**
 * Milliseconds from the tab's relay opening until the session bar names the path, as `Direct`
 * or as the Through server label with its reason; checked every frame on the tab's own clock.
 */
export async function msUntilPath(tab: Page, name: string, timeout: number): Promise<number> {
  const shownAt = await tab.waitForFunction(
    (wanted) =>
      [...document.querySelectorAll("span")].some((span) => (span.getAttribute("aria-label") ?? span.textContent) === wanted)
        ? performance.now()
        : null,
    name,
    { polling: "raf", timeout },
  );
  const relayOpenAt = await tab.evaluate(() => (window as unknown as { relayOpenAt?: number }).relayOpenAt);
  expect(relayOpenAt, "noteRelayOpen ran before the tab opened").toBeDefined();
  return ((await shownAt.jsonValue()) as number) - relayOpenAt!;
}

/** Ends whatever session holds the host, as an administrator; a host with none answers 404. */
export async function endHostSession(request: APIRequestContext, deviceId: string): Promise<void> {
  const resp = await request.delete(`/api/v1/devices/${deviceId}/session`, {
    headers: { Authorization: `Bearer ${adminToken()}` },
  });
  expect([204, 404]).toContain(resp.status());
}
