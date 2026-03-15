import { test as base } from "@playwright/test";
import { createTestUser, injectAuth, type TestUser } from "./helpers/auth-helper";

type Fixtures = {
  testUser: TestUser;
  authedPage: ReturnType<typeof base["page"]> extends Promise<infer P> ? P : never;
};

export const test = base.extend<Fixtures>({
  testUser: async ({ request }, use) => {
    const user = await createTestUser(request);
    await use(user);
  },

  authedPage: async ({ page, testUser }, use) => {
    await page.goto("/");
    await injectAuth(page, testUser.token);
    await page.reload();
    await use(page);
  },
});

export { expect } from "@playwright/test";
