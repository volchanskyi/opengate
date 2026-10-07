import type { APIRequestContext } from "@playwright/test";

// Both stacks name their machines agent-a (read against) and agent-b (the expendable one).
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

export async function enrolledMachine(
  request: APIRequestContext,
  hostname: string,
): Promise<EnrolledMachine> {
  const headers = { Authorization: `Bearer ${adminToken()}` };
  // Stays below the 30s per-test timeout in playwright.config.ts so the throw below is reached.
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
