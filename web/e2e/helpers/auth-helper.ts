import type { Page, APIRequestContext } from "@playwright/test";
import { register, login, getMe } from "./api-helper";

function uniqueEmail(): string {
  const ts = Date.now();
  const rand = Math.random().toString(36).slice(2, 8);
  return `e2e-${ts}-${rand}@test.local`;
}

export interface TestUser {
  id: string;
  email: string;
  password: string;
  token: string;
}

/** Register a fresh user and return credentials + JWT. */
export async function createTestUser(
  request: APIRequestContext
): Promise<TestUser> {
  const email = uniqueEmail();
  const password = "TestPass123!";
  const token = await register(request, email, password);
  const me = await getMe(request, token);
  return { id: me.id, email, password, token };
}

/**
 * Create a user with admin privileges.
 * If this is the first user in a fresh DB, they auto-become admin.
 * Otherwise, the bootstrap admin (from global-setup) promotes them.
 */
export async function createAdminUser(
  request: APIRequestContext
): Promise<TestUser> {
  const email = uniqueEmail();
  const password = "TestPass123!";
  const token = await register(request, email, password);
  const me = await getMe(request, token);

  if (me.is_admin) {
    // First user — already admin via bootstrap
    return { id: me.id, email, password, token };
  }

  // Use bootstrap admin to promote this user via PATCH /api/v1/users/{id}
  const bootstrapToken = await getBootstrapAdminToken(request);
  const patchResp = await request.patch(`/api/v1/users/${me.id}`, {
    data: { is_admin: true },
    headers: { Authorization: `Bearer ${bootstrapToken}` },
  });
  if (!patchResp.ok()) {
    throw new Error(
      `Failed to promote user to admin: ${patchResp.status()} ${await patchResp.text()}`
    );
  }

  // Re-login to get a fresh JWT with admin claim
  const freshToken = await login(request, email, password);
  return { id: me.id, email, password, token: freshToken };
}

/** Get a valid token for the bootstrap admin created in global-setup. */
async function getBootstrapAdminToken(
  request: APIRequestContext
): Promise<string> {
  // Try env var first (same process as global setup)
  if (process.env.BOOTSTRAP_ADMIN_TOKEN) {
    return process.env.BOOTSTRAP_ADMIN_TOKEN;
  }

  // Fallback: login with bootstrap credentials
  const email = process.env.BOOTSTRAP_ADMIN_EMAIL ?? "bootstrap-admin@test.local";
  const password = process.env.BOOTSTRAP_ADMIN_PASSWORD ?? "BootstrapPass123!";
  return login(request, email, password);
}

/** Inject the JWT into localStorage so the SPA treats the session as logged in. */
export async function injectAuth(page: Page, token: string): Promise<void> {
  await page.evaluate((t) => {
    localStorage.setItem("token", t);
  }, token);
}
