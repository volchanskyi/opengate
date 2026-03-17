import { request } from "@playwright/test";

const BASE_URL = "http://localhost:8080";
const BOOTSTRAP_EMAIL = "bootstrap-admin@test.local";
const BOOTSTRAP_PASSWORD = "BootstrapPass123!";

/**
 * Registers the first user in the DB so it auto-promotes to admin.
 * Stores credentials in environment variables for test fixtures.
 */
export default async function globalSetup() {
  const ctx = await request.newContext({ baseURL: BASE_URL });

  try {
    const resp = await ctx.post("/api/v1/auth/register", {
      data: { email: BOOTSTRAP_EMAIL, password: BOOTSTRAP_PASSWORD },
    });

    if (!resp.ok()) {
      // Already exists (e.g., local dev re-run) — try login instead.
      const loginResp = await ctx.post("/api/v1/auth/login", {
        data: { email: BOOTSTRAP_EMAIL, password: BOOTSTRAP_PASSWORD },
      });
      if (!loginResp.ok()) {
        throw new Error(
          `Bootstrap admin setup failed: register=${resp.status()}, login=${loginResp.status()}`
        );
      }
      const body = await loginResp.json();
      process.env.BOOTSTRAP_ADMIN_TOKEN = body.token;
    } else {
      const body = await resp.json();
      process.env.BOOTSTRAP_ADMIN_TOKEN = body.token;
    }

    process.env.BOOTSTRAP_ADMIN_EMAIL = BOOTSTRAP_EMAIL;
    process.env.BOOTSTRAP_ADMIN_PASSWORD = BOOTSTRAP_PASSWORD;
  } finally {
    await ctx.dispose();
  }
}
