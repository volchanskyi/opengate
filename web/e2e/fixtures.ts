import { test as base, type Page } from "@playwright/test";
import { createTestUser, createAdminUser, openSignedIn, type TestUser } from "./helpers/auth-helper";

type Fixtures = {
  testUser: TestUser;
  adminUser: TestUser;
  authedPage: Page;
  adminPage: Page;
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

  // Both page fixtures sign their context in through the console's session endpoint.
  authedPage: async ({ page, testUser }, use) => {
    await openSignedIn(page, testUser);
    await use(page);
  },

  adminPage: async ({ page, adminUser }, use) => {
    await openSignedIn(page, adminUser);
    await use(page);
  },
});

export { expect } from "@playwright/test";
