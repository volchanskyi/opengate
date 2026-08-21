import { defineConfig, devices } from "@playwright/test";

// PR-blocking CI runs only against Chromium. Firefox + WebKit projects are
// gated behind PLAYWRIGHT_ALL_BROWSERS=1 and exercised by the nightly
// `e2e-cross-browser` workflow (see .github/workflows/ci.yml). They do not
// gate merges. WebKit occasionally flakes inside Docker, so the
// cross-browser path bumps retries to 1.
const allBrowsers = process.env.PLAYWRIGHT_ALL_BROWSERS === "1";

export default defineConfig({
  globalSetup: "./e2e/global-setup.ts",
  // Refuses a run that leaves org-visible fleet state behind — see the file for
  // why a leaked group breaks a spec other than the one that leaked it.
  globalTeardown: "./e2e/global-teardown.ts",
  testDir: "./e2e",
  timeout: 30_000,
  retries: allBrowsers ? 1 : 0,
  // The admin-promotion / "last admin" fixtures share server-side IAM state.
  // Two parallel workers running createAdminUser concurrently can land in a
  // window where the PATCH /api/v1/users/{id} commit hasn't propagated before
  // the next worker's /users/me read, surfacing as flaky AdminGuard redirects
  // and false "cannot remove last admin" failures. Serializing eliminates the
  // contention; the trade-off is a small CI runtime increase. CI's e2e job
  // and the local `make e2e` gauntlet now have identical, deterministic
  // ordering — no "passes in CI, flakes locally" gap.
  workers: 1,
  reporter: [["html", { open: "never" }], ["list"]],
  use: {
    baseURL: "http://localhost:8080",
    trace: "on-first-retry",
    serviceWorkers: "block",
  },
  webServer: {
    // The same bring-up `make e2e` runs, so the two paths cannot stand up
    // different stacks. It starts the database and the server, mints an
    // enrolment token through the public endpoint, installs two machines with
    // it, and returns once both are online.
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
