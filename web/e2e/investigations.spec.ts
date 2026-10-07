import { test, expect } from "./fixtures";
import AxeBuilder from "@axe-core/playwright";
import type { Route } from "@playwright/test";

// Repeated filters travel comma-joined, and the room reads only investigation endpoints.

const INCIDENT_ID = "6f2b9c31-1111-4111-8111-444455556666";
const ALERT_ID = "aaaa1111-2222-4333-8444-555566667777";
const DEVICE_ID = "bbbb1111-2222-4333-8444-555566667777";
const ORG_ID = "cccc1111-2222-4333-8444-555566667777";
const USER_ID = "eeee1111-2222-4333-8444-555566667777";

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
    scope: "device",
    scope_key: DEVICE_ID,
    scope_name: "reception-pc",
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
    hostname: "reception-pc",
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
    incident: incident({ assignee_id: USER_ID }),
    alerts: [alert()],
    alerts_total: 1,
    events: [
      { id: "e1", at: "2026-08-12T09:05:00Z", kind: "status_change", body: { from: "new", to: "acknowledged" } },
      { id: "e2", at: "2026-08-12T09:07:00Z", kind: "comment", actor_id: USER_ID, body: { body: "Driver rollout at 02:41" } },
    ],
    events_total: 2,
    people: { [USER_ID]: "Dana Whitfield" },
    ...over,
  };
}

const evidence = {
  ranked: [{ dim: "cpu.busy_pct", score: 0.94 }],
  series: [{ dim: "cpu.busy_pct", points: [{ ts: 1, value: 40 }, { ts: 2, value: 96 }] }],
  processes: [{ rank: 1, basename: "backup-agent", pid: 4242, cpu: 37.5, mem: 121634816 }],
  log_samples: ["<b>kernel</b>: task nginx:1234 blocked for more than 120 seconds"],
  truncated: true,
};

type AuthedPage = Parameters<Parameters<typeof test>[2]>[0]["authedPage"];

// The picker drops a customer the tenant lacks, so the customer comes from the tenant's list.
async function chooseFirstCustomer(page: AuthedPage, token: string): Promise<string> {
  const listed = await page.request.get("/api/v1/organizations", { headers: { Authorization: `Bearer ${token}` } });
  expect(listed.ok()).toBe(true);
  const customers = (await listed.json()) as { id: string }[];
  const customer = customers[0];
  expect(customer).toBeDefined();
  await page.addInitScript((org: string) => {
    localStorage.setItem("selectedOrganizationId", org);
  }, customer!.id);
  return customer!.id;
}

// The pick-lists read the catalogue and the chosen customer's hosts.
async function stubPickLists(page: AuthedPage) {
  await page.route(
    (url: URL) => url.pathname === "/api/v1/rules" || url.pathname === "/api/v1/devices",
    (route: Route) => {
      const url = new URL(route.request().url());
      if (url.pathname === "/api/v1/rules") {
        return ok(route, { fleet_size: 3, rules: [{ id: "cpu.sustained" }, { id: "disk-critical" }] });
      }
      return ok(route, [
        { id: DEVICE_ID, hostname: "reception-pc", status: "online", organization_id: ORG_ID, os: "linux",
          agent_version: "", capabilities: [], last_seen: "", created_at: "", updated_at: "" },
      ]);
    },
  );
}

// Matched by pathname because the room read carries a query string only with a customer selected.
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
  test("the queue opens on New, names what each room is about, and narrows by severity", async ({ authedPage }) => {
    const seen: string[] = [];
    await stubInvestigations(authedPage, seen);

    await authedPage.goto("/investigations");
    await expect(authedPage.getByRole("link", { name: "cpu.sustained" })).toBeVisible();
    await expect(authedPage.getByText("312 alerts")).toBeVisible();
    await expect(authedPage.getByText("40 hosts")).toBeVisible();
    await expect(authedPage.getByText("Host · reception-pc")).toBeVisible();
    await expect(authedPage.getByRole("radio", { name: "New" })).toHaveAttribute("aria-checked", "true");

    expect(new URL(seen[0]!).searchParams.get("status")).toBe("new");

    await authedPage.getByRole("button", { name: "Critical" }).click();
    await expect.poll(() => seen.some((u) => u.includes("severity=critical"))).toBe(true);

    await authedPage.getByRole("radio", { name: "Resolved" }).click();
    await expect.poll(() => seen.some((u) => new URL(u).searchParams.get("status") === "resolved")).toBe(true);
  });

  test("the Rule and Host pick-lists narrow the queue at once", async ({ authedPage, testUser }) => {
    const seen: string[] = [];
    await chooseFirstCustomer(authedPage, testUser.token);
    await stubPickLists(authedPage);
    await stubInvestigations(authedPage, seen);

    await authedPage.goto("/investigations");
    await expect(authedPage.getByRole("link", { name: "cpu.sustained" })).toBeVisible();

    await authedPage.getByLabel("Rule").selectOption("cpu.sustained");
    await expect.poll(() => seen.some((u) => u.includes("rule_id=cpu.sustained"))).toBe(true);

    await authedPage.getByRole("button", { name: /^Host/ }).click();
    await authedPage.getByRole("option", { name: /reception-pc/ }).click();
    await expect.poll(() => seen.some((u) => u.includes(`device_id=${DEVICE_ID}`))).toBe(true);
  });

  test("under All customers the host list waits on a customer and the picker is marked", async ({ authedPage }) => {
    await stubInvestigations(authedPage, []);

    await authedPage.goto("/investigations");
    const host = authedPage.getByRole("button", { name: /^Host/ });
    await expect(host).toBeDisabled();
    await expect(host).toContainText("Pick a customer first");
    await expect(authedPage.getByRole("combobox", { name: "Customer" })).toHaveClass(/ring-2/);
  });

  test("a room renders its history and its evidence by name, and asks nothing of the host", async ({ authedPage }) => {
    const seen: string[] = [];
    await stubInvestigations(authedPage, seen);

    await authedPage.goto("/investigations");
    await authedPage.getByRole("link", { name: "cpu.sustained" }).click();

    await expect(authedPage.getByRole("heading", { name: "cpu.sustained" })).toBeVisible();
    await expect(authedPage.getByText("Held by Dana Whitfield")).toBeVisible();
    await expect(authedPage.getByText("New → Acknowledged")).toBeVisible();
    await expect(authedPage.getByText("Driver rollout at 02:41")).toBeVisible();
    await expect(authedPage.getByRole("list", { name: "Timeline" }).getByText(/by Dana Whitfield/)).toBeVisible();
    await expect(authedPage.getByRole("link", { name: "reception-pc" })).toHaveAttribute("href", `/devices/${DEVICE_ID}`);
    await expect(authedPage.getByText(DEVICE_ID.slice(0, 8))).toHaveCount(0);

    await authedPage.getByRole("button", { name: /Show evidence/ }).click();
    await expect(authedPage.getByRole("list", { name: "Ranked dimensions" })).toBeVisible();
    await expect(authedPage.getByRole("img", { name: /cpu\.busy_pct over the window/ })).toBeVisible();
    const processes = authedPage.getByRole("table", { name: "Processes" });
    await expect(processes).toContainText("backup-agent");
    await expect(processes).toContainText("37.5 %");
    await expect(processes).toContainText("116 MB");
    await expect(authedPage.getByText(/size cap/)).toBeVisible();
    await expect(authedPage.getByText("<b>kernel</b>: task nginx:1234 blocked for more than 120 seconds")).toBeVisible();

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

    await expect(authedPage.getByRole("button", { name: "Confirm resolution" })).toBeDisabled();
    await authedPage.getByLabel("Why it ended").selectOption("false_positive");
    await authedPage.getByRole("button", { name: "Confirm resolution" }).click();

    await expect.poll(() => posted).toEqual({ status: "resolved", cause_code: "false_positive" });
  });

  test("the queue leaves rule coverage to the Rules page", async ({ authedPage }) => {
    await stubInvestigations(authedPage, []);

    await authedPage.goto("/investigations");
    await expect(authedPage.getByRole("link", { name: "cpu.sustained" })).toBeVisible();
    await expect(authedPage.getByRole("button", { name: "Rule coverage" })).toHaveCount(0);
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
