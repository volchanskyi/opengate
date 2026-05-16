import { test, expect } from "./fixtures";

// Visual regression baselines (Chromium only). Cross-browser baselines
// would explode the diff matrix and are intentionally out of scope for
// the cross-browser nightly. `maxDiffPixelRatio: 0.01` tolerates minor
// font-rendering jitter. Update baselines via:
//   cd web && npx playwright test e2e/visual-regression.spec.ts --update-snapshots
// then commit the regenerated PNGs in `web/e2e/__screenshots__/`.
//
// Coverage:
//   - Login page (anonymous)
//   - Register page (anonymous)
//   - Device list, empty (authed)
//   - Admin user management (admin authed) — email column masked because
//     the test fixture seeds an admin with a random UUID suffix; the rest
//     of the table layout (headers, row count, action buttons, badges)
//     is still pixel-asserted.
//
// Excluded:
//   - Admin audit log — content drifts with timestamps + per-run event
//     count; the audit row layout has no stable cell to anchor on. The
//     a11y spec still covers this page.
//   - Heavy specs (device list populated, session view, file manager,
//     filtered device logs) require backend state (live QUIC agent,
//     files in a real sandbox) and are deferred to a follow-up.

const screenshotOptions = { maxDiffPixelRatio: 0.01 } as const;

test.describe("Visual regression (Chromium baselines)", () => {
  test.skip(
    ({ browserName }) => browserName !== "chromium",
    "Visual regression baselines are Chromium-only",
  );

  test("login page", async ({ page }) => {
    await page.goto("/login");
    await expect(page.getByRole("button", { name: "Login" })).toBeVisible();
    await expect(page).toHaveScreenshot("login.png", screenshotOptions);
  });

  test("register page", async ({ page }) => {
    await page.goto("/register");
    await expect(page.getByRole("button", { name: "Register" })).toBeVisible();
    await expect(page).toHaveScreenshot("register.png", screenshotOptions);
  });

  test("device list (empty)", async ({ authedPage }) => {
    await authedPage.goto("/devices");
    await expect(
      authedPage.getByText(/no groups|no devices|create.*group/i),
    ).toBeVisible();
    await expect(authedPage).toHaveScreenshot("device-list-empty.png", screenshotOptions);
  });

  test("admin user management", async ({ adminPage }) => {
    await adminPage.goto("/settings/users");
    await expect(
      adminPage.getByRole("heading", { name: /user management/i }),
    ).toBeVisible();
    // Wait until at least one user row is rendered so the diff is taken on
    // a fully-hydrated table, not the loading state.
    await expect(adminPage.locator('[data-testid="user-email-cell"]').first()).toBeVisible();
    await expect(adminPage).toHaveScreenshot("admin-users.png", {
      ...screenshotOptions,
      // The test fixture seeds an admin user with a random UUID-suffixed
      // email (`admin-<8 hex>@example.com`); without masking the email
      // column the screenshot would drift every run. The rest of the
      // table is asserted pixel-exact.
      mask: [adminPage.locator('[data-testid="user-email-cell"]')],
    });
  });
});
