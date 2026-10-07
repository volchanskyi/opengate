import { test as base } from "@playwright/test";
import { createTestUser, createAdminUser, seedAuth, type TestUser } from "./helpers/auth-helper";

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

  // Both page fixtures seed the token as an init script, so the SPA boots authenticated.
  authedPage: async ({ page, testUser }, use) => {
    await seedAuth(page, testUser.token);
    await Promise.all([
      page.waitForResponse((r) => r.url().includes("/api/v1/users/me") && r.status() === 200),
      page.goto("/"),
    ]);
    await use(page);
  },

  adminPage: async ({ page, adminUser }, use) => {
    await seedAuth(page, adminUser.token);
    await Promise.all([
      page.waitForResponse((r) => r.url().includes("/api/v1/users/me") && r.status() === 200),
      page.goto("/"),
    ]);
    await use(page);
  },
});

export { expect } from "@playwright/test";
