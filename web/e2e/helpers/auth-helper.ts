import type { Page, APIRequestContext } from "@playwright/test";
import { register, getMe } from "./api-helper";

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
 * Otherwise, an existing admin promotes them.
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

  // Not first user — need an existing admin to promote.
  // This path shouldn't happen in test isolation (tmpfs DB), but handle gracefully.
  throw new Error(
    "createAdminUser: user is not admin. Ensure this is the first user in a fresh DB."
  );
}

/** Inject the JWT into localStorage so the SPA treats the session as logged in. */
export async function injectAuth(page: Page, token: string): Promise<void> {
  await page.evaluate((t) => {
    localStorage.setItem("token", t);
  }, token);
}
