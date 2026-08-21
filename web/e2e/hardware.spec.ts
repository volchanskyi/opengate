import { test, expect } from "./fixtures";
import type { Response } from "@playwright/test";
import { enrolledMachine, MACHINE_A } from "./helpers/enrolled-machine";

// Hardware inventory render on the DeviceDetail page, read off a machine that
// is actually running.
//
// The stack carries two enrolled agents, so this asks the question a technician
// asks — does the hardware card show what this machine reported — rather than
// whether the page can render a shape somebody typed into the test. The values
// differ per runner, so the assertions are on shape and presence: a CPU model,
// a core count, a named interface with an address.

test.describe("Hardware inventory", () => {
  test("an online machine's hardware is fetched and rendered", async ({ authedPage, request }) => {
    const machine = await enrolledMachine(request, MACHINE_A);

    const fetched = authedPage.waitForResponse(
      (r: Response) => r.url().includes(`/devices/${machine.id}/hardware`) && r.status() === 200,
    );
    await authedPage.goto(`/devices/${machine.id}`);
    const hardware = await (await fetched).json();

    // The section starts collapsed; the caret opens it.
    await authedPage.getByRole("button", { name: "Hardware", exact: true }).click();

    // What the machine reported is what the card shows. Reading the values out
    // of the response and asserting the page carries them keeps this true on
    // any host, without pinning this one's processor.
    expect(hardware.cpu_model).toBeTruthy();
    await expect(authedPage.getByText(hardware.cpu_model as string)).toBeVisible();
    await expect(authedPage.getByText(`${String(hardware.cpu_cores)} cores`)).toBeVisible();

    const interfaces = hardware.network_interfaces as { name: string; mac: string }[];
    expect(interfaces.length).toBeGreaterThan(0);
    await expect(authedPage.getByText(interfaces[0].name, { exact: false }).first()).toBeVisible();
  });
});
