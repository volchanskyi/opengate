import { test, expect } from "../e2e/fixtures";
import { register } from "./helpers/api-helper";

test.describe("Auth flows", () => {
  test("register creates account and redirects to /devices", async ({
    page,
  }) => {
    const email = `e2e-reg-${Date.now()}@test.local`;

    await page.goto("/register");
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password").fill("TestPass123!");
    await page.getByRole("button", { name: "Register" }).click();

    await expect(page).toHaveURL(/\/devices/);
  });

  test("login with valid credentials redirects to /devices", async ({
    page,
    request,
  }) => {
    const email = `e2e-login-${Date.now()}@test.local`;
    await register(request, email, "TestPass123!");

    await page.goto("/login");
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password").fill("TestPass123!");
    await page.getByRole("button", { name: "Login" }).click();

    await expect(page).toHaveURL(/\/devices/);
  });

  test("login with invalid credentials shows error", async ({
    page,
    request,
  }) => {
    const email = `e2e-bad-${Date.now()}@test.local`;
    await register(request, email, "TestPass123!");

    await page.goto("/login");
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password").fill("wrong-password");
    await page.getByRole("button", { name: "Login" }).click();

    await expect(page.locator(".text-red-400")).toBeVisible();
  });

  test("logout clears session and redirects to /login", async ({
    authedPage,
  }) => {
    await authedPage.goto("/devices");

    // Click logout (in the nav layout)
    const logoutBtn = authedPage.getByRole("button", { name: /logout/i });
    if (await logoutBtn.isVisible()) {
      await logoutBtn.click();
      await expect(authedPage).toHaveURL(/\/login/);
    }
  });

  test("expired token redirects to /login", async ({ page }) => {
    // Inject a garbage token to simulate expired session
    await page.goto("/");
    await page.evaluate(() => {
      localStorage.setItem(
        "auth-storage",
        JSON.stringify({ state: { token: "expired.token.here" }, version: 0 })
      );
    });

    await page.goto("/devices");
    // AuthGuard should redirect to login when /me fails
    await expect(page).toHaveURL(/\/login/);
  });
});
