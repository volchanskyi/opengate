import { test, expect } from "./fixtures";
import type { Response } from "@playwright/test";
import { enrolledMachine, MACHINE_A } from "./helpers/enrolled-machine";

// Hardware values differ per runner, so the page is checked against the machine's own response.

test.describe("Hardware inventory", () => {
  test("an online machine's hardware is fetched and rendered", async ({ authedPage, request }) => {
    const machine = await enrolledMachine(request, MACHINE_A);

    const fetched = authedPage.waitForResponse(
      (r: Response) => r.url().includes(`/devices/${machine.id}/hardware`) && r.status() === 200,
    );
    await authedPage.goto(`/devices/${machine.id}`);
    const hardware = await (await fetched).json();

    await authedPage.getByRole("button", { name: "Hardware", exact: true }).click();

    expect(hardware.cpu_model).toBeTruthy();
    await expect(authedPage.getByText(hardware.cpu_model as string)).toBeVisible();
    await expect(authedPage.getByText(`${String(hardware.cpu_cores)} cores`)).toBeVisible();

    // Scoped to the interface list because a short name such as `lo` also matches hidden <option>s.
    const interfaces = hardware.network_interfaces as { name: string; mac: string }[];
    expect(interfaces.length).toBeGreaterThan(0);
    const list = authedPage.getByRole("list", { name: "Network Interfaces" });
    await expect(list.getByText(`${interfaces[0].name}: ${interfaces[0].mac}`)).toBeVisible();
  });
});
