import { test as base } from "@playwright/test";
import { createTestUser, createAdminUser, injectAuth, type TestUser } from "./helpers/auth-helper";

type Fixtures = {
  testUser: TestUser;
  adminUser: TestUser;
  authedPage: ReturnType<typeof base["page"]> extends Promise<infer P> ? P : never;
  adminPage: ReturnType<typeof base["page"]> extends Promise<infer P> ? P : never;
};

export const test = base.extend<Fixtures>({
  testUser: async ({ request }, use) => {
    const user = await createTestUser(request);
    await use(user);
  },

  adminUser: async ({ request }, use) => {
    const user = await createAdminUser(request);
    await use(user);
  },

  authedPage: async ({ page, testUser }, use) => {
    await page.goto("/");
    await injectAuth(page, testUser.token);
    await Promise.all([
      page.waitForResponse((r) => r.url().includes("/api/v1/users/me") && r.status() === 200),
      page.reload(),
    ]);
    await use(page);
  },

  adminPage: async ({ page, adminUser }, use) => {
    await page.goto("/");
    await injectAuth(page, adminUser.token);
    // Wait for hydration: reload and wait for /users/me to confirm admin status
    await Promise.all([
      page.waitForResponse((r) => r.url().includes("/api/v1/users/me") && r.status() === 200),
      page.reload(),
    ]);
    await use(page);
  },
});

export { expect } from "@playwright/test";
