import { test, expect } from "./fixtures";
import type { Route } from "@playwright/test";

// The rules screen, end to end: the pack, a rule's page, the labels, and the
// alert budget.
//
// Store mapping and the wording helpers are covered by the unit suite. What only
// a browser can show is asserted here: the routes resolve and render, a rule's
// logic never appears as a control, and the whole write surface is absent for an
// ordinary member rather than present-and-refused.

const LABEL_ID = "dddd1111-2222-4333-8444-555566667777";
const DEVICE_ID = "bbbb1111-2222-4333-8444-555566667777";

function ok(route: Route, body: unknown) {
  return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}

function rollout(over: Record<string, unknown> = {}) {
  return {
    enabled: true,
    rollout_percent: 100,
    kill: false,
    stage: "full",
    canary_percent: 1,
    staged_percent: 10,
    canary_hold_secs: 3600,
    staged_hold_secs: 21600,
    ...over,
  };
}

function rule(over: Record<string, unknown> = {}) {
  return {
    id: "disk-critical",
    version: 2,
    summary: "A disk about to fill",
    metric: "disk.used_percent",
    comparator: "gte",
    threshold: 90,
    sustain_secs: 300,
    group_by: ["device"],
    group_window_secs: 300,
    evidence: ["vitals"],
    coverage_requires: ["disk.used_percent"],
    tunable: { threshold: { min: 50, max: 99, shipped: 90 } },
    rollout: rollout(),
    coverage: { active: 300, throttled: 2, unsupported: 6, unknown: 4 },
    noise: { recent: 40, baseline_per_hour: 2, level: "high" },
    ...over,
  };
}

const detail = {
  rule: rule(),
  bindings: [
    {
      id: "bbbb0000-2222-4333-8444-555566667777",
      level: "site",
      level_key: "eeee1111-2222-4333-8444-555566667777",
      selector: { role: "file-server" },
      precedence: 10,
      params: { threshold: 95 },
      updated_by: "ivan",
    },
  ],
  clamps: [
    {
      id: "ffff1111-2222-4333-8444-555566667777",
      binding_id: "bbbb0000-2222-4333-8444-555566667777",
      rule_id: "disk-critical",
      rule_version: 2,
      param: "threshold",
      from_value: 98,
      to_value: 95,
      clamped_at: "2026-08-17T00:00:00Z",
    },
  ],
};

const labels = {
  labels: [{ id: LABEL_ID, key: "role", value: "file-server", created_by: "ivan" }],
  assignments: [{ device_id: DEVICE_ID, tags: { role: "file-server" } }],
};

const limits = {
  organization_hourly: 500,
  device_hourly: 20,
  max_organization_hourly: 5000,
  max_device_hourly: 200,
  updated_by: "ivan",
};

type AuthedPage = Parameters<Parameters<typeof test>[2]>[0]["authedPage"];

/**
 * Stub the pack, one rule, the labels and the budget, recording every URL asked
 * for. Matched by pathname rather than by glob, because each read carries a
 * customer query only when one is selected.
 */
async function stubRules(page: AuthedPage, seen: string[]) {
  await page.route(
    (url: URL) =>
      url.pathname.startsWith("/api/v1/rules") ||
      url.pathname.startsWith("/api/v1/device-tags") ||
      url.pathname.startsWith("/api/v1/alert-limits"),
    (route: Route) => {
      const url = new URL(route.request().url());
      seen.push(url.toString());
      if (url.pathname === "/api/v1/alert-limits") return ok(route, limits);
      if (url.pathname === "/api/v1/device-tags") return ok(route, labels);
      if (url.pathname === "/api/v1/rules/disk-critical") return ok(route, detail);
      if (url.pathname === "/api/v1/rules") {
        return ok(route, { fleet_size: 312, rules: [rule()] });
      }
      return route.continue();
    },
  );
}

test.describe("Rules", () => {
  test("the pack lists what each rule watches, and never as something to edit", async ({ authedPage }) => {
    const seen: string[] = [];
    await stubRules(authedPage, seen);

    await authedPage.goto("/rules");
    await expect(authedPage.getByRole("link", { name: "disk-critical" })).toBeVisible();
    await expect(authedPage.getByText("disk.used_percent at or above 90")).toBeVisible();

    // The count is against the fleet it was taken over, and machines that
    // cannot run the rule are called out rather than folded into the total.
    await expect(authedPage.getByText("/ 312")).toBeVisible();
    await expect(authedPage.getByText(/6 cannot run it/)).toBeVisible();

    // Nothing on the list is a control.
    await expect(authedPage.locator("input")).toHaveCount(0);
  });

  test("a rule's page explains its tuning, its coverage and what a version change moved", async ({
    authedPage,
  }) => {
    const seen: string[] = [];
    await stubRules(authedPage, seen);

    await authedPage.goto("/rules");
    await authedPage.getByRole("link", { name: "disk-critical" }).click();

    await expect(authedPage.getByRole("heading", { name: "disk-critical" })).toBeVisible();
    await expect(authedPage.getByText("machines labelled role=file-server")).toBeVisible();
    await expect(authedPage.getByText(/allowed 50–99, ships at 90/).first()).toBeVisible();

    // The clamp says what moved and that the rule is still firing at the moved
    // value, which is the point of it.
    await expect(authedPage.getByText(/no longer allows threshold at 98/)).toBeVisible();
    await expect(authedPage.getByText(/it is running at 95/)).toBeVisible();

    // Coverage accounts for every machine in the fleet.
    await expect(authedPage.getByText("312 of 312 machines accounted for.")).toBeVisible();
  });

  test("an ordinary member has the whole page to read and nothing to press", async ({ authedPage }) => {
    const seen: string[] = [];
    await stubRules(authedPage, seen);

    await authedPage.goto("/rules/disk-critical");
    await expect(authedPage.getByText("A disk about to fill")).toBeVisible();

    for (const name of ["Stop for this customer", "Stop for every customer", "Save pace", "Understood"]) {
      await expect(authedPage.getByRole("button", { name })).toHaveCount(0);
    }
  });

  test("an administrator gets the stop switch, apart from the on-off toggle", async ({ adminPage }) => {
    const seen: string[] = [];
    await stubRules(adminPage, seen);

    await adminPage.goto("/rules/disk-critical");
    await expect(adminPage.getByRole("button", { name: "Stop for this customer" })).toBeVisible();
    await expect(adminPage.getByRole("button", { name: "Stop for every customer" })).toBeVisible();
    await expect(adminPage.getByText("This customer gets the rule")).toBeVisible();

    // The pull-back is not something the screen offers a way to switch off.
    await expect(adminPage.getByText(/cannot be\s+switched off/)).toBeVisible();
  });

  test("the labels page lists what each label covers", async ({ authedPage }) => {
    const seen: string[] = [];
    await stubRules(authedPage, seen);

    await authedPage.goto("/rules/labels");
    await expect(authedPage.getByText("role=file-server").first()).toBeVisible();
    expect(seen.some((u) => u.includes("/api/v1/device-tags"))).toBe(true);
  });

  test("the alert budget shows both ceilings with how far each may be raised", async ({ authedPage }) => {
    const seen: string[] = [];
    await stubRules(authedPage, seen);

    await authedPage.goto("/rules/alert-limits");
    await expect(authedPage.getByLabel("This customer, per hour")).toHaveValue("500");
    await expect(authedPage.getByLabel("One machine, per hour")).toHaveValue("20");
    await expect(authedPage.getByText(/At most 5000/)).toBeVisible();
    await expect(authedPage.getByText(/Enforced on the machine itself/)).toBeVisible();
  });

  test("the customer picker narrows every read on the screen", async ({
    authedPage,
    request,
    testUser,
  }) => {
    const seen: string[] = [];

    // A real customer, read from the tenant's own list with the caller's token.
    // The picker deliberately drops a selection the tenant does not have, so a
    // made-up id would be cleared on boot and the reads would go out unnarrowed
    // — the app working as designed rather than the narrowing failing.
    const listed = await request.get("/api/v1/organizations", {
      headers: { Authorization: `Bearer ${testUser.token}` },
    });
    expect(listed.ok()).toBe(true);
    const customers = (await listed.json()) as { id: string }[];
    const customer = customers[0];
    expect(customer).toBeDefined();

    await stubRules(authedPage, seen);
    await authedPage.addInitScript((org: string) => {
      localStorage.setItem("selectedOrganizationId", org);
    }, customer!.id);

    await authedPage.goto("/rules");
    await expect(authedPage.getByRole("link", { name: "disk-critical" })).toBeVisible();
    await expect
      .poll(() => seen.some((u) => u.includes(`organization_id=${customer!.id}`)))
      .toBe(true);
  });
});
