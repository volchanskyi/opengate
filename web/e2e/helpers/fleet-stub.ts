import type { Page, Route } from "@playwright/test";

// All e2e users share one organization, so a test asserting an empty fleet stubs it itself.
export async function stubEmptyFleet(page: Page): Promise<void> {
  const empty = (route: Route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: "[]" });

  await page.route("**/api/v1/sites", empty);
  await page.route("**/api/v1/devices", empty);
  await page.route("**/api/v1/devices?**", empty);
}
