import type { APIRequestContext } from "@playwright/test";

/**
 * The machines the stacks under test actually run.
 *
 * Two of them, so a spec can disturb one without breaking every other spec:
 * agent-a is what the device pages are read against, agent-b is the expendable
 * one. Both stacks that run this suite bring them up under these names —
 * deploy/docker-compose.test.yml pins them as container hostnames, and the
 * staging deploy creates two pods so named, a pod's hostname being its name.
 * scripts/tests/e2e-stack-machines.test.sh holds all three files together.
 */
export const MACHINE_A = "agent-a";
export const MACHINE_B = "agent-b";

export interface EnrolledMachine {
  id: string;
  hostname: string;
  status: string;
  os: string;
  capabilities: string[];
  site_id: string;
  organization_id: string;
}

/**
 * The bootstrap operator's credential, which global-setup.ts puts in the
 * environment. Every helper that has to look past what a spec's own user can
 * see reads it from here rather than reaching for the variable itself.
 */
export function adminToken(): string {
  const token = process.env.BOOTSTRAP_ADMIN_TOKEN;
  if (!token) {
    throw new Error(
      "BOOTSTRAP_ADMIN_TOKEN is unset, so the enrolled machines cannot be looked up. " +
        "global-setup.ts sets it, so this means setup did not complete.",
    );
  }
  return token;
}

/**
 * Returns the machine with the given hostname, once it is online.
 *
 * Values differ per runner — the CPU model, the RAM, the addresses — so a spec
 * asks for the machine by the one thing that is pinned, and asserts on shape
 * rather than on what this particular host happens to be.
 */
export async function enrolledMachine(
  request: APIRequestContext,
  hostname: string,
): Promise<EnrolledMachine> {
  const headers = { Authorization: `Bearer ${adminToken()}` };
  // Below the 30s per-test timeout in playwright.config.ts, so the throw below
  // is reached and says what the fleet held. Given the same 30s, fourteen
  // staging runs died on a bare timeout instead.
  const deadline = Date.now() + 20_000;

  let lastSeen = "nothing";
  while (Date.now() < deadline) {
    const resp = await request.get("/api/v1/devices", { headers });
    if (resp.ok()) {
      const machines: EnrolledMachine[] = await resp.json();
      lastSeen =
        machines.map((m) => `${m.hostname}=${m.status}`).join(", ") ||
        "an empty fleet";
      const found = machines.find(
        (m) => m.hostname === hostname && m.status === "online",
      );
      if (found) return found;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }

  throw new Error(
    `the machine ${hostname} never came online. The fleet holds: ${lastSeen}. ` +
      "Both stacks install the machines and wait for them before the suite starts — " +
      "deploy/scripts/e2e-stack-up.sh locally, the staging deploy job in " +
      ".github/workflows/cd.yml — so this means one of them dropped off during the run.",
  );
}
