import { defineConfig, devices } from "@playwright/test";

// PR-blocking CI runs only against Chromium. Firefox + WebKit projects are
// gated behind PLAYWRIGHT_ALL_BROWSERS=1 and exercised by the nightly
// `e2e-cross-browser` workflow (see .github/workflows/ci.yml). They do not
// gate merges. WebKit occasionally flakes inside Docker, so the
// cross-browser path bumps retries to 1.
const allBrowsers = process.env.PLAYWRIGHT_ALL_BROWSERS === "1";

export default defineConfig({
  globalSetup: "./e2e/global-setup.ts",
  globalTeardown: "./e2e/global-teardown.ts",
  testDir: "./e2e",
  timeout: 30_000,
  retries: allBrowsers ? 1 : 0,
  reporter: [["html", { open: "never" }], ["list"]],
  use: {
    baseURL: "http://localhost:8080",
    trace: "on-first-retry",
    serviceWorkers: "block",
  },
  webServer: {
    command:
      "cd ../deploy && docker compose -f docker-compose.test.yml down -v 2>/dev/null; docker compose -f docker-compose.test.yml up --build --wait",
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
