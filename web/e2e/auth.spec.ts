import { test, expect } from "./fixtures";
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

  test("a session cookie the server does not know redirects to /login", async ({ page, baseURL }) => {
    await page.context().addCookies([{ name: "og_session", value: "forged.value", url: baseURL! }]);

    await page.goto("/devices");
    await expect(page).toHaveURL(/\/login/);
  });

  test("the console keeps no pass where a script can read it", async ({ authedPage }) => {
    const stored = await authedPage.evaluate(() => ({
      local: Object.keys(localStorage).map((k) => localStorage.getItem(k) ?? ""),
      session: Object.keys(sessionStorage).map((k) => sessionStorage.getItem(k) ?? ""),
      cookies: document.cookie,
    }));
    const jwtShape = /eyJ[\w-]+\.[\w-]+\.[\w-]+/;
    for (const value of [...stored.local, ...stored.session]) {
      expect(value).not.toMatch(jwtShape);
    }
    expect(stored.cookies).not.toContain("og_session");
    const cookie = (await authedPage.context().cookies()).find((c) => c.name === "og_session");
    expect(cookie?.httpOnly).toBe(true);
    expect(cookie?.sameSite).toBe("Strict");
  });

  test("logout redirects to /login and makes the old cookie useless", async ({ authedPage, request }) => {
    const before = (await authedPage.context().cookies()).find((c) => c.name === "og_session");
    expect(before).toBeDefined();
    await authedPage.goto("/devices");
    await authedPage.getByRole("button", { name: /logout/i }).click();
    await expect(authedPage).toHaveURL(/\/login/);

    const replayed = await request.get("/api/v1/users/me", {
      headers: { Cookie: `og_session=${before!.value}` },
    });
    expect(replayed.status()).toBe(401);
  });
});
