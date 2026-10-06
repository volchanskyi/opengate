import { test, expect } from "./fixtures";
import type { Request, Route } from "@playwright/test";
import { enrolledMachine, MACHINE_B } from "./helpers/enrolled-machine";

// A real restart would re-enroll agent-b as a second row, since its data directory is a tmpfs.

function ok(route: Route, body: unknown) {
  return route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

function fakeSession(DEVICE_ID: string) {
  const now = new Date().toISOString();
  return {
    token: "e2e-restart-session-0000000000000000000000000000",
    device_id: DEVICE_ID,
    relay_url: "wss://relay.invalid/relay",
    status: "active",
    created_at: now,
    updated_at: now,
  };
}

type AuthedPage = Parameters<Parameters<typeof test>[2]>[0]["authedPage"];

async function answerRestartWith(
  page: AuthedPage,
  DEVICE_ID: string,
  opts: { sessions?: unknown[]; restartStatus?: number } = {},
) {
  const { sessions, restartStatus = 200 } = opts;
  if (sessions) {
    await page.route(`**/api/v1/sessions?device_id=${DEVICE_ID}*`, (route: Route) =>
      ok(route, sessions),
    );
  }
  await page.route(`**/api/v1/devices/${DEVICE_ID}/restart`, (route: Route) => {
    if (route.request().method() !== "POST") return route.fallback();
    if (restartStatus >= 400) {
      // openapi-fetch fills `error` only when the body parses as the ApiError schema.
      return route.fulfill({
        status: restartStatus,
        contentType: "application/json",
        body: JSON.stringify({ error: "agent not connected" }),
      });
    }
    return route.fulfill({ status: restartStatus, body: "" });
  });
}

test.describe("Restart Agent flow", () => {
  test("restart with no active sessions sends immediately and toasts success", async ({
    authedPage,
    request,
  }) => {
    const DEVICE_ID = (await enrolledMachine(request, MACHINE_B)).id;
    await answerRestartWith(authedPage, DEVICE_ID);
    await authedPage.goto(`/devices/${DEVICE_ID}`);

    const sent = authedPage.waitForRequest(
      (r: Request) => r.url().includes(`/devices/${DEVICE_ID}/restart`) && r.method() === "POST",
    );
    await authedPage.getByRole("button", { name: "Restart Agent" }).click();
    await sent;

    await expect(authedPage.getByRole("alert").filter({ hasText: "Restart command sent" })).toBeVisible();
  });

  test("active sessions require a second confirm click before sending", async ({
    authedPage,
    request,
  }) => {
    const DEVICE_ID = (await enrolledMachine(request, MACHINE_B)).id;
    await answerRestartWith(authedPage, DEVICE_ID, { sessions: [fakeSession(DEVICE_ID)] });
    await authedPage.goto(`/devices/${DEVICE_ID}`);

    let restartPosts = 0;
    await authedPage.route(`**/api/v1/devices/${DEVICE_ID}/restart`, (route: Route) => {
      restartPosts += 1;
      return route.fulfill({ status: 200, body: "" });
    });

    await authedPage.getByRole("button", { name: "Restart Agent" }).click();
    await expect(authedPage.getByRole("button", { name: /confirm \(1 active\)/i })).toBeVisible();
    expect(restartPosts).toBe(0);

    await authedPage.getByRole("button", { name: /confirm \(1 active\)/i }).click();
    await expect(authedPage.getByRole("alert").filter({ hasText: "Restart command sent" })).toBeVisible();
    expect(restartPosts).toBe(1);
  });

  test("a failed restart surfaces an error toast", async ({ authedPage, request }) => {
    const DEVICE_ID = (await enrolledMachine(request, MACHINE_B)).id;
    await answerRestartWith(authedPage, DEVICE_ID, { restartStatus: 409 });
    await authedPage.goto(`/devices/${DEVICE_ID}`);

    await authedPage.getByRole("button", { name: "Restart Agent" }).click();
    await expect(authedPage.getByRole("alert").filter({ hasText: "Failed to restart agent" })).toBeVisible();
  });
});
