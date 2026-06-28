import { test, expect } from "./fixtures";
import type { Request, Response, Route } from "@playwright/test";

// Web Push subscribe flow (NotificationCenter bell).
//
// Scope, per audit-tests-coverage.md F1: real browser push *delivery* needs an
// OS-level push service and is out of automated scope (the Playwright config
// also sets serviceWorkers: "block"). This spec covers the deterministic
// *subscribe* half — VAPID-key fetch on mount, then a click driving
// pushManager.subscribe → POST /api/v1/push/subscribe — by injecting a fake
// service worker + PushManager so no real push service is required. Server-side
// subscribe handling is unit-tested in push_handlers_test.go.

// A valid base64url VAPID public key (65 bytes) so urlBase64ToUint8Array/atob
// in NotificationCenter succeeds.
const VAPID_KEY =
  "BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8";
const FAKE_ENDPOINT = "https://push.example.invalid/sub/e2e-abc";

// installFakePush makes the SW + PushManager APIs deterministic (the real ones
// are blocked by serviceWorkers: "block"). pushManager.subscribe resolves to a
// fixed subscription so the subscribe POST carries known values.
async function installFakePush(
  page: Parameters<Parameters<typeof test>[2]>[0]["authedPage"],
) {
  await page.addInitScript(
    ({ endpoint }) => {
      const sub = {
        endpoint,
        toJSON: () => ({ endpoint, keys: { p256dh: "p256dh-e2e", auth: "auth-e2e" } }),
        unsubscribe: async () => true,
      };
      const registration = {
        pushManager: {
          getSubscription: async () => null,
          subscribe: async () => sub,
        },
      };
      Object.defineProperty(navigator, "serviceWorker", {
        configurable: true,
        get: () => ({
          ready: Promise.resolve(registration),
          register: async () => registration,
          getRegistration: async () => registration,
          addEventListener: () => {},
        }),
      });
      if (!("PushManager" in globalThis)) {
        (globalThis as unknown as { PushManager: unknown }).PushManager = function () {};
      }
    },
    { endpoint: FAKE_ENDPOINT },
  );
}

test.describe("Web Push subscribe flow", () => {
  test("fetches the VAPID key and POSTs a subscription on enable", async ({
    authedPage,
  }) => {
    await installFakePush(authedPage);

    let vapidFetched = false;
    await authedPage.route("**/api/v1/push/vapid-key", (route: Route) => {
      vapidFetched = true;
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ public_key: VAPID_KEY }),
      });
    });
    await authedPage.route("**/api/v1/push/subscribe", (route: Route) => {
      if (route.request().method() !== "POST") return route.fallback();
      return route.fulfill({ status: 201, body: "" });
    });

    // Re-load so the init script + routes apply and NotificationCenter mounts.
    await Promise.all([
      authedPage.waitForResponse(
        (r: Response) => r.url().includes("/api/v1/push/vapid-key") && r.status() === 200,
      ),
      authedPage.reload(),
    ]);
    expect(vapidFetched).toBe(true);

    const subscribePost = authedPage.waitForRequest(
      (r: Request) => r.url().includes("/api/v1/push/subscribe") && r.method() === "POST",
    );
    await authedPage.getByRole("button", { name: "Enable notifications" }).click();
    const req = await subscribePost;

    expect(req.postDataJSON()).toMatchObject({
      endpoint: FAKE_ENDPOINT,
      p256dh: "p256dh-e2e",
      auth: "auth-e2e",
    });

    // Bell reflects the subscribed state.
    await expect(
      authedPage.getByRole("button", { name: "Disable notifications" }),
    ).toBeVisible();
  });
});
