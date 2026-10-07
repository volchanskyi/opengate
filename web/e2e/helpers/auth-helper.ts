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

// Init scripts re-run on every navigation, so the SPA boots authenticated on its first load.
export async function seedAuth(page: Page, token: string): Promise<void> {
  await page.addInitScript((t) => {
    localStorage.setItem("token", t);
  }, token);
}
