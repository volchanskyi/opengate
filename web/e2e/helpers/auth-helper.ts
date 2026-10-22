import type { Page, APIRequestContext, BrowserContext } from "@playwright/test";
import { register, login, getMe } from "./api-helper";

function uniqueEmail(): string {
  return `e2e-${Date.now()}-${crypto.randomUUID().slice(0, 8)}@test.local`;
}

export interface TestUser {
  id: string;
  email: string;
  password: string;
  token: string;
}

export async function createTestUser(
  request: APIRequestContext
): Promise<TestUser> {
  const email = uniqueEmail();
  const password = "TestPass123!";
  const token = await register(request, email, password);
  const me = await getMe(request, token);
  return { id: me.id, email, password, token };
}

export async function createAdminUser(
  request: APIRequestContext
): Promise<TestUser> {
  const email = uniqueEmail();
  const password = "TestPass123!";
  const token = await register(request, email, password);
  const me = await getMe(request, token);

  if (me.is_admin) {
    return { id: me.id, email, password, token };
  }

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

  // A fresh login returns a JWT that carries the admin claim.
  const freshToken = await login(request, email, password);
  return { id: me.id, email, password, token: freshToken };
}

async function getBootstrapAdminToken(
  request: APIRequestContext
): Promise<string> {
  if (process.env.BOOTSTRAP_ADMIN_TOKEN) {
    return process.env.BOOTSTRAP_ADMIN_TOKEN;
  }

  const email = process.env.BOOTSTRAP_ADMIN_EMAIL ?? "bootstrap-admin@test.local";
  const password = process.env.BOOTSTRAP_ADMIN_PASSWORD ?? "BootstrapPass123!";
  return login(request, email, password);
}

/**
 * Signs the browser context in through the console's own session endpoint, so every page it opens
 * carries the session cookie, exactly as after the sign-in form.
 */
export async function signInBrowser(
  context: BrowserContext,
  user: Pick<TestUser, "email" | "password">
): Promise<void> {
  const resp = await context.request.post("/api/v1/auth/session", {
    data: { email: user.email, password: user.password },
  });
  if (!resp.ok()) {
    throw new Error(`Console sign-in failed: ${resp.status()} ${await resp.text()}`);
  }
}

/** Signs the page's context in and opens the console, once its session answers. */
export async function openSignedIn(page: Page, user: TestUser): Promise<void> {
  await signInBrowser(page.context(), user);
  await Promise.all([
    page.waitForResponse((r) => r.url().endsWith("/api/v1/auth/session") && r.status() === 200),
    page.goto("/"),
  ]);
}
