import { defineConfig, devices } from "@playwright/test";

// Firefox and WebKit run only under PLAYWRIGHT_ALL_BROWSERS=1, in the nightly workflow, and retry
// once, since WebKit occasionally flakes inside Docker.
const allBrowsers = process.env.PLAYWRIGHT_ALL_BROWSERS === "1";
// The speed specs run alone under PLAYWRIGHT_PERFORMANCE=1, in Chromium, in their nightly workflow.
const speedRun = process.env.PLAYWRIGHT_PERFORMANCE === "1";
// This spec drives the browser server behind the stack's home router.
const TWO_ROUTERS = /two-routers\.spec\.ts$/;
// The speed specs time sessions over the routers' shaped lines; one reads Chromium's internals.
const SPEED = /performance(-chromium)?\.spec\.ts$/;

/** A project whose browser runs on the browser server behind the stack's home router. */
function behindHomeRouter(browser: string, device: string) {
  return {
    name: `${browser}-behind-home-router`,
    testMatch: TWO_ROUTERS,
    use: {
      ...devices[device],
      // The home router forwards its outer address's port 3000 to the browser server behind it.
      connectOptions: { wsEndpoint: "ws://10.77.0.3:3000/" },
      baseURL: "http://10.77.0.10:8080",
    },
  };
}

export default defineConfig({
  globalSetup: "./e2e/global-setup.ts",
  // Refuses a run that leaves organization-visible fleet state behind.
  globalTeardown: "./e2e/global-teardown.ts",
  testDir: "./e2e",
  timeout: 30_000,
  retries: allBrowsers ? 1 : 0,
  // One worker serializes the admin-promotion fixtures, which share server-side IAM state.
  workers: 1,
  // The JSON report is what scripts/e2e-summarize.sh reads into a nightly run's summary.
  reporter: [["html", { open: "never" }], ["list"], ["json", { outputFile: "test-results/e2e-report.json" }]],
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
  projects: speedRun
    ? [{ name: "performance", use: { ...devices["Desktop Chrome"] }, testMatch: SPEED }]
    : [
        {
          name: "chromium",
          use: { ...devices["Desktop Chrome"] },
          testIgnore: [TWO_ROUTERS, SPEED],
        },
        behindHomeRouter("chromium", "Desktop Chrome"),
        ...(allBrowsers
          ? [
              {
                name: "firefox",
                use: { ...devices["Desktop Firefox"] },
                testIgnore: [TWO_ROUTERS, SPEED],
              },
              {
                name: "webkit",
                use: { ...devices["Desktop Safari"] },
                testIgnore: [TWO_ROUTERS, SPEED],
              },
              behindHomeRouter("firefox", "Desktop Firefox"),
              behindHomeRouter("webkit", "Desktop Safari"),
            ]
          : []),
      ],
});
