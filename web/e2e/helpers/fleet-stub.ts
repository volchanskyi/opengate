import type { Page, Route } from "@playwright/test";

/**
 * Serve an empty fleet to the page under test.
 *
 * The organization is the visibility boundary for groups and devices, and every
 * e2e user registers into the same one. "The fleet is empty" is therefore a
 * property of the entire suite, not of one test: any spec that seeds a group
 * changes what every later spec's device page renders. A test that asserts an
 * empty-fleet UI must supply that emptiness itself, so it asserts on the
 * rendering it is actually about rather than on the order the suite happened to
 * run in.
 */
export async function stubEmptyFleet(page: Page): Promise<void> {
  const empty = (route: Route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: "[]" });

  await page.route("**/api/v1/groups", empty);
  await page.route("**/api/v1/devices", empty);
  await page.route("**/api/v1/devices?**", empty);
}
