import { test, expect } from "./fixtures";

// The alert pack, against the real stack: the real server, its own compiled
// rule file, and the real machines that enrolled into it.
//
// Nothing here is stubbed, which is the point. Every other rules spec fulfils
// the reads in the browser, so it proves the screen and never the pack behind
// it — and the pack is what decides whether an alert a machine raises is
// something the product can accept, place in a room, and let somebody stop.
//
// What is deliberately not here is watching a real machine cross a real line.
// Every shipped rule's lowest settable boundary is 50, and nothing in this
// stack decides what a container's own readings actually are, so a test that
// waited for one would pass or fail on the runner's disk. That path is driven
// where it can be driven exactly: the machine's own producer in
// `alert_producer_test.rs`, and the crossing into a room in the integration
// tier.

/** Rules that watch the machine's own log records rather than a reading. */
const WORD_RULES = [
  "linux-oom-kill",
  "linux-hung-task",
  "linux-ata-reset",
  "linux-thermal-throttle",
  "linux-service-errors",
];

/** Rules that compare one of the machine's numbers against a line. */
const READING_RULES = [
  "disk-critical",
  "cpu-saturated",
  "memory-pressure",
  "io-stalled",
  "disk-slow",
];

type Rule = {
  id: string;
  kind: "reading" | "event";
  severity: string;
  summary: string;
  metric?: string;
  threshold?: number;
  tunable: Record<string, unknown>;
};

test.describe("the rules the product actually ships", () => {
  test("every rule the machines can raise is one the product knows about", async ({
    adminPage,
  }) => {
    const reply = await adminPage.request.get("/api/v1/rules");
    expect(reply.status()).toBe(200);
    const { rules } = (await reply.json()) as { rules: Rule[] };

    const ids = rules.map((r) => r.id);
    for (const id of [...READING_RULES, ...WORD_RULES]) {
      // A rule the server has never heard of has every alert it raises
      // refused, which is indistinguishable from a machine that raised none.
      expect(ids, `${id} must be a rule this build ships`).toContain(id);
    }

    for (const rule of rules) {
      expect(rule.severity, `${rule.id} must say how bad it is`).toBeTruthy();
    }
  });

  test("a rule watching the machine's own words has no numbers to retune", async ({
    adminPage,
  }) => {
    const reply = await adminPage.request.get("/api/v1/rules");
    const { rules } = (await reply.json()) as { rules: Rule[] };
    const byId = new Map(rules.map((r) => [r.id, r]));

    for (const id of WORD_RULES) {
      const rule = byId.get(id);
      expect(rule?.kind, `${id} watches the machine's own words`).toBe("event");
      // Showing a boundary of nought would read as a setting somebody chose.
      expect(rule?.metric, `${id} names no reading`).toBeUndefined();
      expect(rule?.threshold, `${id} has no line to cross`).toBeUndefined();
      expect(Object.keys(rule?.tunable ?? {}), `${id} has nothing to retune`).toHaveLength(0);
    }

    for (const id of READING_RULES) {
      const rule = byId.get(id);
      expect(rule?.kind, `${id} watches a reading`).toBe("reading");
      expect(rule?.metric, `${id} must name the reading it watches`).toBeTruthy();
    }
  });

  test("the screen shows both kinds, and says what each one watches", async ({ adminPage }) => {
    await adminPage.goto("/rules");

    const rows = adminPage.locator("table tbody tr");
    await expect(rows).toHaveCount(READING_RULES.length + WORD_RULES.length);

    // A rule about a reading reads as the comparison it makes.
    const readingRow = rows.filter({ hasText: "disk-critical" });
    await expect(readingRow).toContainText("disk.used_percent");

    // A rule about words reads as what it means, because there is no
    // comparison to show — and "undefined at or above undefined" would read as
    // a rule somebody left half-written.
    const wordRow = rows.filter({ hasText: "linux-oom-kill" });
    await expect(wordRow).toContainText("memory");
    await expect(wordRow).not.toContainText("undefined");
    await expect(wordRow).not.toContainText("at or above");
  });

  test("an administrator can stop a rule the machines carry themselves", async ({
    adminPage,
  }) => {
    const ruleId = "linux-thermal-throttle";

    const stopped = await adminPage.request.post(`/api/v1/rules/${ruleId}/stop`, {
      data: { scope: "organization", stopped: true },
    });
    expect(stopped.status(), await stopped.text()).toBe(204);

    try {
      await adminPage.goto("/rules");
      const row = adminPage.locator("table tbody tr").filter({ hasText: ruleId });
      await expect(row).toContainText("Stopped");
    } finally {
      // Put it back, so a spec that runs after this one reads the pack as it
      // ships rather than as this one left it.
      const resumed = await adminPage.request.post(`/api/v1/rules/${ruleId}/stop`, {
        data: { scope: "organization", stopped: false },
      });
      expect(resumed.status()).toBe(204);
    }
  });

  test("a real machine says it sends its alerts with the evidence attached", async ({
    adminPage,
  }) => {
    const reply = await adminPage.request.get("/api/v1/devices");
    expect(reply.status()).toBe(200);
    const devices = (await reply.json()) as { hostname: string; capabilities: string[] }[];

    const machines = devices.filter((d) => d.hostname.startsWith("agent-"));
    expect(machines.length, "the stack must hold the machines that enrolled into it").toBeGreaterThan(0);

    for (const machine of machines) {
      // Telling the product this is what lets it expect an alert to arrive
      // carrying everything behind it, rather than expecting to ask later.
      expect(machine.capabilities, `${machine.hostname} sends its own alerts`).toContain("Alerts");
    }
  });
});
