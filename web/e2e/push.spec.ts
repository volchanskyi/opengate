import { test, expect } from "./fixtures";
import type { Request, Route } from "@playwright/test";

// Real push delivery needs an OS push service, so a fake service worker and PushManager stand in.

// A valid base64url VAPID public key (65 bytes), so NotificationCenter decodes it.
const VAPID_KEY =
  "BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8";
const FAKE_ENDPOINT = "https://push.example.invalid/sub/e2e-abc";

// The service worker APIs are blocked, so subscribe resolves to a fixed subscription.
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

    await authedPage.route("**/api/v1/push/vapid-key", (route: Route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ public_key: VAPID_KEY }),
      }),
    );
    await authedPage.route("**/api/v1/push/subscribe", (route: Route) => {
      if (route.request().method() !== "POST") return route.fallback();
      return route.fulfill({ status: 201, body: "" });
    });

    // Awaiting the VAPID-key request itself is one signal that cannot race a handler-set flag.
    const [vapidRequest] = await Promise.all([
      authedPage.waitForRequest((r: Request) =>
        r.url().includes("/api/v1/push/vapid-key"),
      ),
      authedPage.reload(),
    ]);
    expect(vapidRequest.method()).toBe("GET");

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

    await expect(
      authedPage.getByRole("button", { name: "Disable notifications" }),
    ).toBeVisible();
  });
});
