import { test, expect } from "./fixtures";
import type { APIRequestContext, Request } from "@playwright/test";

// Organization is the visibility boundary; is_admin is the mutation boundary.
// Every member of an organization sees the same fleet and may command any of
// its devices; only configuration is gated on admin.

function auth(token: string) {
  return { headers: { Authorization: `Bearer ${token}` } };
}

async function groupIds(request: APIRequestContext, token: string): Promise<string[]> {
  const resp = await request.get("/api/v1/groups", auth(token));
  expect(resp.status()).toBe(200);
  const groups: { id: string }[] = await resp.json();
  return groups.map((g) => g.id);
}

// A group is visible to the whole organization, so one left behind here would
// surface in another spec's "empty device list" assertion and its screenshot
// baseline. Track what each test creates and remove it afterwards, pass or fail.
const createdGroupIds: string[] = [];

async function seedGroup(
  request: APIRequestContext,
  adminToken: string,
  name: string,
): Promise<string> {
  const resp = await request.post("/api/v1/groups", { ...auth(adminToken), data: { name } });
  expect(resp.status()).toBe(201);
  const group: { id: string } = await resp.json();
  createdGroupIds.push(group.id);
  return group.id;
}

/** Drops an id a test already deleted itself, so the hook does not re-delete it. */
function forgetGroup(id: string): void {
  const at = createdGroupIds.indexOf(id);
  if (at !== -1) {
    createdGroupIds.splice(at, 1);
  }
}

test.describe("Authorization model", () => {
  test.afterEach(async ({ request, adminUser }) => {
    for (const id of createdGroupIds.splice(0)) {
      await request.delete(`/api/v1/groups/${id}`, auth(adminUser.token));
    }
  });

  test("fleet reads are open to every member of the organization", async ({
    request,
    testUser,
    adminUser,
  }) => {
    const memberDevices = await request.get("/api/v1/devices", auth(testUser.token));
    expect(memberDevices.status()).toBe(200);

    const adminDevices = await request.get("/api/v1/devices", auth(adminUser.token));
    expect(adminDevices.status()).toBe(200);

    // Group listing is a fleet read, not an ownership query.
    expect((await request.get("/api/v1/groups", auth(testUser.token))).status()).toBe(200);
  });

  test("a member sees every group in the organization, including one they never created", async ({
    request,
    testUser,
    adminUser,
  }) => {
    const id = await seedGroup(request, adminUser.token, `e2e-authz-${Date.now().toString()}`);

    expect(await groupIds(request, testUser.token)).toContain(id);

    // And the detail read is open too.
    const detail = await request.get(`/api/v1/groups/${id}`, auth(testUser.token));
    expect(detail.status()).toBe(200);
  });

  test("group configuration is refused to a non-admin member", async ({
    request,
    testUser,
    adminUser,
  }) => {
    const create = await request.post("/api/v1/groups", {
      ...auth(testUser.token),
      data: { name: `e2e-authz-denied-${Date.now().toString()}` },
    });
    expect(create.status()).toBe(403);

    const id = await seedGroup(request, adminUser.token, `e2e-authz-admin-${Date.now().toString()}`);

    const memberDelete = await request.delete(`/api/v1/groups/${id}`, auth(testUser.token));
    expect(memberDelete.status()).toBe(403);

    const adminDelete = await request.delete(`/api/v1/groups/${id}`, auth(adminUser.token));
    expect(adminDelete.status()).toBe(204);
    // Already gone — drop it so the cleanup hook does not chase a dead id.
    forgetGroup(id);
  });

  test("secret-bearing reads stay admin-only for a member", async ({ request, testUser }) => {
    for (const path of [
      "/api/v1/enrollment-tokens",
      "/api/v1/audit",
      "/api/v1/users",
      "/api/v1/security-groups",
    ]) {
      const resp = await request.get(path, auth(testUser.token));
      expect(resp.status(), `${path} must stay admin-only`).toBe(403);
    }
  });

  test("a non-admin's device list page hides the group-configuration controls", async ({
    authedPage,
  }) => {
    await authedPage.goto("/devices");
    await expect(authedPage.getByRole("heading", { name: "Groups" })).toBeVisible();
    // Absent from the DOM, not merely disabled.
    await expect(authedPage.getByText("+ New")).toHaveCount(0);
    await expect(authedPage.getByText(/drag a device card onto a group/i)).toHaveCount(0);
  });

  test("an admin's device list page shows them", async ({ adminPage }) => {
    await adminPage.goto("/devices");
    await expect(adminPage.getByText("+ New")).toBeVisible();
  });
});

test.describe("Fleet summary endpoint", () => {
  test("returns a fixed-size rollup to any member", async ({ request, testUser }) => {
    const resp = await request.get("/api/v1/devices/summary", auth(testUser.token));
    expect(resp.status()).toBe(200);

    const body: Record<string, unknown> = await resp.json();
    expect(Object.keys(body).sort()).toEqual([
      "health",
      "maintenance",
      "offline",
      "online",
      "total",
    ]);
    expect(Object.keys(body.health as object).sort()).toEqual([
      "anomalous",
      "healthy",
      "unknown",
      "watch",
    ]);
    expect(body.total).toBe((body.online as number) + (body.offline as number));
  });

  test("the static route wins over /devices/{id}", async ({ request, testUser }) => {
    const resp = await request.get("/api/v1/devices/summary", auth(testUser.token));
    expect(resp.status()).toBe(200);
    // A device payload would carry a hostname; the summary never does.
    expect(await resp.text()).not.toContain("hostname");
  });

  test("the dashboard issues one summary request per poll and never the device list", async ({
    authedPage,
  }) => {
    const summaryCalls: string[] = [];
    const listCalls: string[] = [];
    authedPage.on("request", (req: Request) => {
      const url = req.url();
      if (url.includes("/api/v1/devices/summary")) summaryCalls.push(url);
      else if (/\/api\/v1\/devices(\?|$)/.test(url)) listCalls.push(url);
    });

    await authedPage.goto("/");
    await expect(authedPage.getByRole("heading", { name: "Dashboard" })).toBeVisible();
    await expect.poll(() => summaryCalls.length).toBeGreaterThan(0);

    expect(listCalls, "the dashboard must not download the device table").toEqual([]);
  });
});
