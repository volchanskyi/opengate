import { request, type FullConfig } from "@playwright/test";

interface Group {
  id: string;
  name: string;
}

/**
 * Fails the run if a spec left a group behind in the shared organization.
 *
 * The organization is the visibility boundary for groups and devices, and every
 * e2e user registers into the same one, so a group a spec forgets to delete is
 * visible to every spec that runs after it. The damage does not land on the
 * spec that leaked: it lands on whichever later spec asserts that the fleet is
 * empty, which reads as an unrelated regression and moves with run order.
 *
 * Failing here keeps the leak attributable to the run that caused it. The
 * leftovers are also deleted, so the next run against the same database starts
 * from the state it expects instead of inheriting the previous run's failure.
 */
export default async function globalTeardown(config: FullConfig) {
  const baseURL = config.projects[0]?.use?.baseURL ?? "http://localhost:8080";
  const token = process.env.BOOTSTRAP_ADMIN_TOKEN;
  if (!token) {
    throw new Error(
      "Group-leak check cannot run: BOOTSTRAP_ADMIN_TOKEN is unset. " +
        "global-setup.ts sets it, so this means setup did not complete.",
    );
  }

  const ctx = await request.newContext({ baseURL });
  try {
    const headers = { Authorization: `Bearer ${token}` };
    const resp = await ctx.get("/api/v1/groups", { headers });
    if (!resp.ok()) {
      throw new Error(
        `Group-leak check could not list groups: ${resp.status().toString()} ${await resp.text()}`,
      );
    }

    const groups: Group[] = await resp.json();
    if (groups.length === 0) return;

    for (const group of groups) {
      await ctx.delete(`/api/v1/groups/${group.id}`, { headers });
    }

    const names = groups.map((g) => g.name).join(", ");
    throw new Error(
      `${groups.length.toString()} group(s) outlived the suite: ${names}. ` +
        "A group is visible to the whole organization, so it changes what every " +
        "later spec's device page renders. Delete what a spec creates in an " +
        "afterEach hook, or stub the fleet endpoints instead of seeding real " +
        "groups (see e2e/helpers/fleet-stub.ts). The leftovers have been removed.",
    );
  } finally {
    await ctx.dispose();
  }
}
