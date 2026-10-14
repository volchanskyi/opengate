import { test, expect } from "./fixtures";
import type { Browser, Page } from "@playwright/test";
import { createAdminUser, createTestUser, type TestUser } from "./helpers/auth-helper";
import { adminToken, enrolledMachine, MACHINE_A } from "./helpers/enrolled-machine";
import { endHostSession, openSessionTab } from "./helpers/session-tab";

// One technician per host: the session tab, who holds a host, and who may end it.

async function signedIn(browser: Browser, user: TestUser): Promise<Page> {
  const context = await browser.newContext({ baseURL: test.info().project.use.baseURL });
  await context.addInitScript((t) => localStorage.setItem("token", t), user.token);
  return context.newPage();
}

async function echoes(tab: Page, tag: string): Promise<void> {
  await tab.locator('[data-testid="terminal-container"]').click();
  await tab.keyboard.type(`echo opengate-$((6*7))-${tag}\n`);
  await expect(tab.locator(".xterm-rows")).toContainText(`opengate-42-${tag}`, { timeout: 15_000 });
}

test.describe("Session tab", () => {
  test.afterEach(async ({ request }) => {
    await endHostSession(request, (await enrolledMachine(request, MACHINE_A)).id);
  });

  test("the tab needs nothing from the page that opened it", async ({ authedPage, request }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    const tab = await openSessionTab(authedPage, machine.id);
    await authedPage.close();
    await echoes(tab, "orphan");

    // Opened by its address alone, with no opener at all, it replaces the session it finds.
    const direct = await tab.context().newPage();
    await direct.goto(`/devices/${machine.id}/session`);
    await echoes(direct, "by-address");
    await expect(tab.getByText("Opened elsewhere")).toBeVisible({ timeout: 5_000 });
  });

  test("a reload starts a new session, which replaces the old", async ({ authedPage, request }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    const sessions = async () => {
      const resp = await request.get(`/api/v1/sessions?device_id=${machine.id}`, {
        headers: { Authorization: `Bearer ${adminToken()}` },
      });
      return ((await resp.json()) as { created_at: string }[]).map((s) => s.created_at);
    };
    const tab = await openSessionTab(authedPage, machine.id);
    await echoes(tab, "first");
    const first = await sessions();

    await tab.reload();
    await echoes(tab, "second");
    const second = await sessions();
    expect(first).toHaveLength(1);
    expect(second).toHaveLength(1);
    expect(second[0]).not.toBe(first[0]);
  });

  test("Go to session reaches the open tab without reloading it", async ({ authedPage, request }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    const tab = await openSessionTab(authedPage, machine.id);
    await echoes(tab, "kept");
    await tab.evaluate(() => { (window as unknown as { mark: string }).mark = "kept"; });

    let opened = 0;
    authedPage.context().on("page", () => { opened++; });
    await authedPage.reload();
    await authedPage.getByRole("button", { name: "Go to session" }).click();

    await authedPage.waitForTimeout(1_000);
    expect(opened).toBe(0);
    expect(await tab.evaluate(() => (window as unknown as { mark?: string }).mark)).toBe("kept");
  });

  test("another technician sees who holds the host; an administrator ends it and the host opens again", async ({
    authedPage,
    browser,
    request,
  }) => {
    test.setTimeout(60_000);
    const machine = await enrolledMachine(request, MACHINE_A);
    const holderTab = await openSessionTab(authedPage, machine.id);
    await echoes(holderTab, "held");

    const other = await signedIn(browser, await createTestUser(request));
    await other.goto(`/devices/${machine.id}`);
    await expect(other.getByText(/^Session open — .+, since \d{1,2}:\d{2}/).first()).toBeVisible();
    await expect(other.getByRole("button", { name: "Open session" })).toBeDisabled();
    await expect(other.getByRole("button", { name: "End session" })).toHaveCount(0);
    await expect(other.getByText("an administrator can end it")).toBeVisible();

    const admin = await signedIn(browser, await createAdminUser(request));
    await admin.goto(`/devices/${machine.id}`);
    await admin.getByRole("button", { name: "End session" }).click();
    await admin.getByRole("button", { name: /^Confirm: end .+’s session$/ }).click();

    await expect(holderTab.getByText(/^Ended by /)).toBeVisible({ timeout: 5_000 });
    await other.reload();
    const otherTab = await openSessionTab(other, machine.id);
    await echoes(otherTab, "reopened");
    await other.context().close();
    await admin.context().close();
  });

  test("the session tab ignores the fleet tab's customer, and ends when the fleet tab signs out", async ({
    authedPage,
    request,
  }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    const tab = await openSessionTab(authedPage, machine.id);
    await echoes(tab, "before");
    const title = await tab.title();
    expect(title).toMatch(new RegExp(`^${MACHINE_A} — .+`));

    await authedPage.goto("/devices");
    const picker = authedPage.getByRole("combobox", { name: "Customer" });
    const current = await picker.inputValue();
    const options = await picker
      .locator("option")
      .evaluateAll((all: Element[]) => all.map((o) => (o as HTMLOptionElement).value));
    await picker.selectOption(options.find((value: string) => value !== current)!);

    await echoes(tab, "after-customer");
    expect(await tab.title()).toBe(title);

    const signedOut = Date.now();
    await authedPage.getByRole("button", { name: /logout/i }).click();
    await expect(tab.getByText("Signed out")).toBeVisible({ timeout: 2_000 });
    expect(Date.now() - signedOut).toBeLessThan(2_000);
    await expect
      .poll(async () => {
        const resp = await request.get(`/api/v1/devices/${machine.id}`, {
          headers: { Authorization: `Bearer ${adminToken()}` },
        });
        return ((await resp.json()) as { session?: unknown }).session ?? null;
      })
      .toBeNull();
  });
});
