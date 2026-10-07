import { test, expect } from "./fixtures";

// Runs against the real server and its compiled rule pack; nothing is stubbed.

const WORD_RULES = [
  "linux-oom-kill",
  "linux-hung-task",
  "linux-ata-reset",
  "linux-thermal-throttle",
  "linux-service-errors",
];

const READING_RULES = [
  "disk-critical",
  "cpu-saturated",
  "memory-pressure",
  "io-stalled",
  "disk-slow",
];

function auth(token: string) {
  return { headers: { Authorization: `Bearer ${token}` } };
}

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
    request,
    adminUser,
  }) => {
    const reply = await request.get("/api/v1/rules", auth(adminUser.token));
    expect(reply.status()).toBe(200);
    const { rules } = (await reply.json()) as { rules: Rule[] };

    const ids = rules.map((r) => r.id);
    for (const id of [...READING_RULES, ...WORD_RULES]) {
      expect(ids, `${id} must be a rule this build ships`).toContain(id);
    }

    for (const rule of rules) {
      expect(rule.severity, `${rule.id} must say how bad it is`).toBeTruthy();
    }
  });

  test("a rule watching the machine's own words has no numbers to retune", async ({
    request,
    adminUser,
  }) => {
    const reply = await request.get("/api/v1/rules", auth(adminUser.token));
    expect(reply.status()).toBe(200);
    const { rules } = (await reply.json()) as { rules: Rule[] };
    const byId = new Map(rules.map((r) => [r.id, r]));

    for (const id of WORD_RULES) {
      const rule = byId.get(id);
      expect(rule?.kind, `${id} watches the machine's own words`).toBe("event");
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

    const readingRow = rows.filter({ hasText: "disk-critical" });
    await expect(readingRow).toContainText("disk.used_percent");

    const wordRow = rows.filter({ hasText: "linux-oom-kill" });
    await expect(wordRow).toContainText("memory");
    await expect(wordRow).not.toContainText("undefined");
    await expect(wordRow).not.toContainText("at or above");
  });

  test("an administrator can stop a rule the machines carry themselves", async ({
    adminPage,
    request,
    adminUser,
  }) => {
    const ruleId = "linux-thermal-throttle";

    const stopped = await request.post(`/api/v1/rules/${ruleId}/stop`, {
      ...auth(adminUser.token),
      data: { scope: "organization", stopped: true },
    });
    expect(stopped.status(), await stopped.text()).toBe(204);

    try {
      await adminPage.goto("/rules");
      const row = adminPage.locator("table tbody tr").filter({ hasText: ruleId });
      await expect(row).toContainText("Stopped");
    } finally {
      // Resumes the rule so later specs read the shipped pack.
      const resumed = await request.post(`/api/v1/rules/${ruleId}/stop`, {
        ...auth(adminUser.token),
        data: { scope: "organization", stopped: false },
      });
      expect(resumed.status()).toBe(204);
    }
  });

  test("a real machine says it sends its alerts with the evidence attached", async ({
    request,
    adminUser,
  }) => {
    const reply = await request.get("/api/v1/devices", auth(adminUser.token));
    expect(reply.status()).toBe(200);
    const devices = (await reply.json()) as { hostname: string; capabilities: string[] }[];

    const machines = devices.filter((d) => d.hostname.startsWith("agent-"));
    expect(machines.length, "the stack must hold the machines that enrolled into it").toBeGreaterThan(0);

    for (const machine of machines) {
      expect(machine.capabilities, `${machine.hostname} sends its own alerts`).toContain("Alerts");
    }
  });
});
