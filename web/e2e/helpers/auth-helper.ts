import type { Page, APIRequestContext } from "@playwright/test";
import { register } from "./api-helper";

function uniqueEmail(): string {
  const ts = Date.now();
  const rand = Math.random().toString(36).slice(2, 8);
  return `e2e-${ts}-${rand}@test.local`;
}

export interface TestUser {
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
  return { email, password, token };
}

/** Inject the JWT into localStorage so the SPA treats the session as logged in. */
export async function injectAuth(page: Page, token: string): Promise<void> {
  await page.evaluate((t) => {
    localStorage.setItem("token", t);
  }, token);
}
