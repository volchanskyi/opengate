import { request, type FullConfig } from "@playwright/test";

const BOOTSTRAP_EMAIL = "bootstrap-admin@test.local";
const BOOTSTRAP_PASSWORD = "BootstrapPass123!";

// Registers the first user, which the server promotes to admin, and exports its credentials.
export default async function globalSetup(config: FullConfig) {
  const baseURL = config.projects[0]?.use?.baseURL ?? "http://localhost:8080";
  const ctx = await request.newContext({ baseURL });

  try {
    const resp = await ctx.post("/api/v1/auth/register", {
      data: { email: BOOTSTRAP_EMAIL, password: BOOTSTRAP_PASSWORD },
    });

    if (!resp.ok()) {
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

    // A stale database promoted a different first user, so the admin claim is verified.
    const meResp = await ctx.get("/api/v1/users/me", {
      headers: { Authorization: `Bearer ${process.env.BOOTSTRAP_ADMIN_TOKEN}` },
    });
    if (meResp.ok()) {
      const me = await meResp.json();
      if (!me.is_admin) {
        throw new Error(
          "Bootstrap admin is not admin — DB may be stale. " +
            "Run: cd deploy && docker compose -f docker-compose.test.yml down -v"
        );
      }
    }
  } finally {
    await ctx.dispose();
  }
}
