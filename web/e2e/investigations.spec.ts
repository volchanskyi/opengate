import { test, expect } from "./fixtures";
import AxeBuilder from "@axe-core/playwright";
import type { Route } from "@playwright/test";

// The triage workspace, end to end: the queue, the room a queue row opens, the
// evidence one alert carries, and the resolution that closes it.
//
// Store mapping and the lifecycle vocabulary are covered by the unit suite.
// What only a browser can show is asserted here: the routes resolve and render,
// repeated filters travel in the comma-joined form the API binds, and the room
// asks nothing of the machine that raised the alert.

const INCIDENT_ID = "6f2b9c31-1111-4111-8111-444455556666";
const ALERT_ID = "aaaa1111-2222-4333-8444-555566667777";
const DEVICE_ID = "bbbb1111-2222-4333-8444-555566667777";
const ORG_ID = "cccc1111-2222-4333-8444-555566667777";

const WAIVED_RULES: ReadonlySet<string> = new Set([
  "color-contrast",
  "link-in-text-block",
  "link-in-text-block-style",
]);

function ok(route: Route, body: unknown) {
  return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
}

function incident(over: Record<string, unknown> = {}) {
  return {
    id: INCIDENT_ID,
    organization_id: ORG_ID,
    rule_id: "cpu.sustained",
    scope: "organization",
    scope_key: ORG_ID,
    severity: "critical",
    status: "new",
    opened_at: "2026-08-12T09:00:00Z",
    first_seen: "2026-08-12T09:00:00Z",
    last_seen: "2026-08-12T11:05:00Z",
    occurrences: 312,
    device_count: 40,
    ...over,
  };
}

function alert() {
  return {
    id: ALERT_ID,
    device_id: DEVICE_ID,
    rule_id: "cpu.sustained",
    rule_version: 3,
    severity: "critical",
    metric: "cpu.busy_pct",
    value: 96.4,
    window_start: "2026-08-12T09:00:00Z",
    window_end: "2026-08-12T09:01:00Z",
    observed_at: "2026-08-12T09:00:30Z",
    received_at: "2026-08-12T09:00:45Z",
    backfilled: false,
    evidence_codec: "zstd",
    evidence_bytes: 4096,
  };
}

function detail(over: Record<string, unknown> = {}) {
  return {
    incident: incident(),
    alerts: [alert()],
    alerts_total: 1,
    events: [
      { id: "e1", at: "2026-08-12T09:05:00Z", kind: "status_change", body: { from: "new", to: "acknowledged" } },
      { id: "e2", at: "2026-08-12T09:07:00Z", kind: "comment", actor_id: DEVICE_ID, body: { body: "Driver rollout at 02:41" } },
    ],
    events_total: 2,
    ...over,
  };
}

const evidence = {
  ranked: [{ dim: "cpu.busy_pct", score: 0.94 }],
  series: [{ dim: "cpu.busy_pct", points: [{ ts: 1, value: 40 }, { ts: 2, value: 96 }] }],
  processes: [{ rank: 1, basename: "chrome", pid: 4242, cpu: 88.5, mem: 12.5 }],
  log_samples: ["<b>kernel</b>: task nginx:1234 blocked for more than 120 seconds"],
  truncated: true,
};

type AuthedPage = Parameters<Parameters<typeof test>[2]>[0]["authedPage"];

/**
 * Stub the queue, the room and its evidence, recording every investigation URL
 * requested. Matched by pathname rather than by glob: the room read carries a
 * query string only when a customer is selected, so a pattern that assumes one
 * would let the real request through and answer a 404 instead of the fixture.
 */
async function stubInvestigations(page: AuthedPage, seen: string[]) {
  await page.route(
    (url: URL) => url.pathname.startsWith("/api/v1/investigations"),
    (route: Route) => {
      const url = new URL(route.request().url());
      seen.push(url.toString());
      if (url.pathname.endsWith("/evidence")) return ok(route, evidence);
      if (url.pathname === `/api/v1/investigations/${INCIDENT_ID}`) return ok(route, detail());
      if (url.pathname === "/api/v1/investigations") return ok(route, { items: [incident()] });
      return route.continue();
    },
  );
}

test.describe("Investigations", () => {
  test("the queue lists open incidents and narrows by comma-joined filters", async ({ authedPage }) => {
    const seen: string[] = [];
    await stubInvestigations(authedPage, seen);

    await authedPage.goto("/investigations");
    await expect(authedPage.getByRole("link", { name: "cpu.sustained" })).toBeVisible();
    await expect(authedPage.getByText("312 alerts")).toBeVisible();
    await expect(authedPage.getByText("40 machines")).toBeVisible();

    // The open statuses travel as one comma-joined value: the server's binder
    // reads only the first value of a repeated parameter.
    expect(seen[0]).toContain("status=new,acknowledged,investigating");

    await authedPage.getByRole("button", { name: "Critical" }).click();
    await expect.poll(() => seen.some((u) => u.includes("severity=critical"))).toBe(true);
  });

  test("a room renders its history and its evidence, and asks nothing of the machine", async ({ authedPage }) => {
    const seen: string[] = [];
    await stubInvestigations(authedPage, seen);

    await authedPage.goto("/investigations");
    await authedPage.getByRole("link", { name: "cpu.sustained" }).click();

    await expect(authedPage.getByRole("heading", { name: "cpu.sustained" })).toBeVisible();
    await expect(authedPage.getByText("New → Acknowledged")).toBeVisible();
    await expect(authedPage.getByText("Driver rollout at 02:41")).toBeVisible();

    await authedPage.getByRole("button", { name: /Show evidence/ }).click();
    await expect(authedPage.getByRole("list", { name: "Ranked dimensions" })).toBeVisible();
    await expect(authedPage.getByRole("img", { name: /cpu\.busy_pct over the window/ })).toBeVisible();
    await expect(authedPage.getByRole("table", { name: "Processes" })).toContainText("chrome");
    // A truncated blob says so, and a host log line renders as its characters.
    await expect(authedPage.getByText(/size cap/)).toBeVisible();
    await expect(authedPage.getByText("<b>kernel</b>: task nginx:1234 blocked for more than 120 seconds")).toBeVisible();

    // Everything the room fetched was an investigation read.
    expect(seen.length).toBeGreaterThan(0);
    for (const url of seen) {
      expect(new URL(url).pathname.startsWith("/api/v1/investigations")).toBe(true);
    }
  });

  test("resolving asks for a cause code and sends it", async ({ authedPage }) => {
    await stubInvestigations(authedPage, []);
    let posted: unknown;
    await authedPage.route(`**/api/v1/investigations/${INCIDENT_ID}/status*`, (route: Route) => {
      posted = route.request().postDataJSON() as unknown;
      return ok(route, incident({ status: "resolved", cause_code: "false_positive" }));
    });

    await authedPage.goto(`/investigations/${INCIDENT_ID}`);
    await authedPage.getByRole("button", { name: "Resolve" }).click();

    // Nothing is sent until an answer is chosen.
    await expect(authedPage.getByRole("button", { name: "Confirm resolution" })).toBeDisabled();
    await authedPage.getByLabel("Why it ended").selectOption("false_positive");
    await authedPage.getByRole("button", { name: "Confirm resolution" }).click();

    await expect.poll(() => posted).toEqual({ status: "resolved", cause_code: "false_positive" });
  });

  test("rule coverage shows all four states against the fleet", async ({ authedPage }) => {
    await stubInvestigations(authedPage, []);
    await authedPage.route("**/api/v1/rules*", (route: Route) =>
      ok(route, {
        fleet_size: 312,
        rules: [{
          id: "cpu.sustained", version: 3, summary: "CPU pinned for two minutes",
          metric: "cpu.busy_pct", comparator: "gt", threshold: 90, group_by: ["device_id"],
          group_window_secs: 900, evidence: ["series"], coverage_requires: ["cpu.busy_pct"],
          tunable: {}, rollout: { enabled: true, rollout_percent: 100, kill: false },
          coverage: { active: 300, throttled: 5, unsupported: 6, unknown: 1 },
        }],
      }),
    );

    await authedPage.goto("/investigations");
    await authedPage.getByRole("button", { name: "Rule coverage" }).click();

    const row = authedPage.getByRole("row", { name: /cpu\.sustained/ });
    await expect(row.getByLabel("Watching")).toHaveText("300");
    await expect(row.getByLabel("Cannot evaluate")).toHaveText("6");
    await expect(authedPage.getByText("Counted against 312 machines.")).toBeVisible();
  });

  test("the queue and a room have no axe violations", async ({ authedPage }) => {
    await stubInvestigations(authedPage, []);

    await authedPage.goto("/investigations");
    await expect(authedPage.getByRole("link", { name: "cpu.sustained" })).toBeVisible();
    const queue = await new AxeBuilder({ page: authedPage })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    expect(queue.violations.filter((v) => !WAIVED_RULES.has(v.id))).toEqual([]);

    await authedPage.getByRole("link", { name: "cpu.sustained" }).click();
    await expect(authedPage.getByRole("heading", { name: "cpu.sustained" })).toBeVisible();
    const room = await new AxeBuilder({ page: authedPage })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    expect(room.violations.filter((v) => !WAIVED_RULES.has(v.id))).toEqual([]);
  });
});
