import type { APIRequestContext } from "@playwright/test";

/**
 * The machines the test stack actually runs.
 *
 * deploy/docker-compose.test.yml starts two agents and pins their hostnames,
 * so a spec can name the machine it is about. agent-a is what the device pages
 * are read against; agent-b is the expendable one, for a spec that wants to
 * disturb a machine without breaking every other spec.
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
  const deadline = Date.now() + 30_000;

  let lastSeen = "nothing";
  while (Date.now() < deadline) {
    const resp = await request.get("/api/v1/devices", { headers });
    if (resp.ok()) {
      const machines: EnrolledMachine[] = await resp.json();
      lastSeen = machines.map((m) => `${m.hostname}=${m.status}`).join(", ") || "an empty fleet";
      const found = machines.find((m) => m.hostname === hostname && m.status === "online");
      if (found) return found;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }

  throw new Error(
    `the machine ${hostname} never came online. The fleet holds: ${lastSeen}. ` +
      "deploy/scripts/e2e-stack-up.sh installs both machines and waits for them, " +
      "so this means one of them dropped off during the run.",
  );
}
