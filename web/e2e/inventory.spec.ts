import { test, expect } from "./fixtures";
import type { Response } from "@playwright/test";
import { enrolledMachine, MACHINE_A } from "./helpers/enrolled-machine";

// Discovered-footprint (inventory) render on the DeviceDetail page, against a
// machine that is actually running.
//
// What a machine discovers about itself is a property of the host it runs on —
// a container's footprint is not a workstation's — so this asserts the round
// trip rather than a fixed set of rows: the page asks the machine's inventory
// endpoint on mount, the answer is the shape the client renders from, and the
// section renders it. Grouping and sort behaviour are covered by
// inventory-store.test.ts and DeviceInventory.test.tsx, which can pin values
// because they supply them.

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

    // The section renders whichever it is: the counts of what the machine
    // found, or the line that says it has found nothing yet. A container's
    // footprint is legitimately empty, and saying so is the honest render.
    await expect(authedPage.getByRole("heading", { name: "Discovered Footprint" })).toBeVisible();
    const items = inventory.items as unknown[];
    if (items.length === 0) {
      await expect(authedPage.getByText(/No footprint discovered yet/i)).toBeVisible();
    } else {
      await expect(authedPage.getByText(/^Discovered: /)).toBeVisible();
    }
  });
});
