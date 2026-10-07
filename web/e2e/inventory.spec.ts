import { test, expect } from "./fixtures";
import type { Response } from "@playwright/test";
import { enrolledMachine, MACHINE_A } from "./helpers/enrolled-machine";

// A machine's discovered footprint depends on its host, so the spec asserts the fetch and render.

test.describe("Discovered footprint", () => {
  test("an online machine's footprint is fetched and rendered", async ({ authedPage, request }) => {
    const machine = await enrolledMachine(request, MACHINE_A);

    const fetched = authedPage.waitForResponse(
      (r: Response) => r.url().includes(`/devices/${machine.id}/inventory`) && r.status() === 200,
    );
    await authedPage.goto(`/devices/${machine.id}`);
    const inventory = await (await fetched).json();

    expect(inventory.device_id).toBe(machine.id);
    expect(Array.isArray(inventory.items)).toBe(true);

    // A container's footprint can be empty, so either render is accepted.
    await expect(authedPage.getByRole("heading", { name: "Discovered Footprint" })).toBeVisible();
    const items = inventory.items as unknown[];
    if (items.length === 0) {
      await expect(authedPage.getByText(/No footprint discovered yet/i)).toBeVisible();
    } else {
      await expect(authedPage.getByText(/^Discovered: /)).toBeVisible();
    }
  });
});
