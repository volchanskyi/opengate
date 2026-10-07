import { defineConfig, devices } from "@playwright/test";

// Firefox and WebKit projects run only under PLAYWRIGHT_ALL_BROWSERS=1, in the nightly workflow.
// WebKit occasionally flakes inside Docker, so the cross-browser path retries once.
const allBrowsers = process.env.PLAYWRIGHT_ALL_BROWSERS === "1";

export default defineConfig({
  globalSetup: "./e2e/global-setup.ts",
  // Refuses a run that leaves organization-visible fleet state behind.
  globalTeardown: "./e2e/global-teardown.ts",
  testDir: "./e2e",
  timeout: 30_000,
  retries: allBrowsers ? 1 : 0,
  // One worker serializes the admin-promotion fixtures, which share server-side IAM state.
  workers: 1,
  reporter: [["html", { open: "never" }], ["list"]],
  use: {
    baseURL: "http://localhost:8080",
    trace: "on-first-retry",
    serviceWorkers: "block",
  },
  webServer: {
    // The same bring-up `make e2e` runs: database, server, enrolment token and two online machines.
    command:
      "cd ../deploy && docker compose -f docker-compose.test.yml down -v 2>/dev/null; bash scripts/e2e-stack-up.sh",
    url: "http://localhost:8080/api/v1/health",
    reuseExistingServer: true,
    timeout: 180_000,
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
    ...(allBrowsers
      ? [
          {
            name: "firefox",
            use: { ...devices["Desktop Firefox"] },
          },
          {
            name: "webkit",
            use: { ...devices["Desktop Safari"] },
          },
        ]
      : []),
  ],
});
