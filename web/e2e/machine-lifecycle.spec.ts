import type { APIRequestContext } from "@playwright/test";
import { test, expect } from "./fixtures";
import {
  MACHINE_C,
  MACHINE_C_COPY,
  copyDataFolder,
  cutPower,
  dataFolder,
  deleteDevice,
  deviceById,
  devices,
  installNote,
  joinCodeUses,
  machineLog,
  machineState,
  mintJoinCode,
  placeFile,
  startMachine,
  stopMachine,
  wipeDataFolder,
  type Device,
} from "./helpers/machine-lifecycle";

// The real agent against the real server: a machine deleted, reinstalled, cut off mid-install and
// cloned, as a technician meets each, judged by what the console and the machine's folder show.

const NOTE = "install-note.json";

/** Every device number the console lists. */
async function listedIds(request: APIRequestContext): Promise<Set<string>> {
  return new Set((await devices(request)).map((d) => d.id));
}

/** Machine C's devices the console lists outside `known`. */
async function newDevices(request: APIRequestContext, known: Set<string>): Promise<Device[]> {
  return (await devices(request)).filter((d) => d.hostname === MACHINE_C && !known.has(d.id));
}

/** Machine C's device online outside `known`, told by number since the clock can step back. */
async function cameOnline(request: APIRequestContext, known: Set<string>): Promise<Device> {
  let found: Device | undefined;
  await expect
    .poll(
      async () => {
        found = (await newDevices(request, known)).find((d) => d.status === "online");
        return found !== undefined;
      },
      { timeout: 30_000 },
    )
    .toBe(true);
  return found!;
}

/** A freshly installed agent-c, online under a number of its own. */
async function liveMachine(request: APIRequestContext): Promise<Device> {
  stopMachine(MACHINE_C);
  wipeDataFolder(MACHINE_C);
  const known = await listedIds(request);
  startMachine(MACHINE_C);
  return cameOnline(request, known);
}

test.describe("Machine lifecycle", () => {
  test.setTimeout(120_000);

  test.afterEach(async ({ request }) => {
    stopMachine(MACHINE_C);
    stopMachine(MACHINE_C_COPY);
    for (const device of await devices(request)) {
      if (device.hostname === MACHINE_C || device.hostname === MACHINE_C_COPY) {
        await deleteDevice(request, device.id);
      }
    }
  });

  test("a machine deleted while switched off removes itself when it comes back", async ({ request }) => {
    const machine = await liveMachine(request);
    stopMachine(MACHINE_C);
    await deleteDevice(request, machine.id);
    const known = await listedIds(request);
    startMachine(MACHINE_C);

    await expect.poll(() => machineState(MACHINE_C).running, { timeout: 30_000 }).toBe(false);
    expect(machineState(MACHINE_C).exitCode, "the removal ends with exit 0, so nothing restarts it").toBe(0);
    expect(machineLog(MACHINE_C)).toContain("device deregistered by server");
    expect(dataFolder(MACHINE_C)).toEqual([]);
    expect(await deviceById(request, machine.id)).toBeNull();
    expect(await newDevices(request, known), "the device does not come back").toEqual([]);
  });

  test("a machine reinstalled over its deleted device comes back under a new number", async ({ request }) => {
    const machine = await liveMachine(request);
    const code = await mintJoinCode(request, "e2e reinstall over a deleted machine");
    const known = await listedIds(request);
    stopMachine(MACHINE_C);
    await deleteDevice(request, machine.id);
    placeFile(MACHINE_C, NOTE, await installNote(request, code.token, false));
    const since = Date.now() - 1_000;
    startMachine(MACHINE_C);

    const reinstalled = await cameOnline(request, known);
    expect(reinstalled.id).not.toBe(machine.id);
    expect(await deviceById(request, machine.id), "the old number stays deleted").toBeNull();
    await expect.poll(() => dataFolder(MACHINE_C).includes(NOTE), { timeout: 15_000 }).toBe(false);

    const now = Date.now();
    const history = await request.get(`/api/v1/devices/${reinstalled.id}/history`, {
      headers: { Authorization: `Bearer ${process.env.BOOTSTRAP_ADMIN_TOKEN}` },
      params: { dim: "cpu.total", from: new Date(now - 3_600_000).toISOString(), to: new Date(now).toISOString() },
    });
    expect(history.ok(), await history.text()).toBe(true);
    const { points } = (await history.json()) as { points: { ts: number }[] };
    expect(points.filter((p) => p.ts * 1000 < since), "no reading from before the wipe").toEqual([]);
  });

  test("a repair reinstall keeps the device and spends no join code", async ({ request }) => {
    const machine = await liveMachine(request);
    const code = await mintJoinCode(request, "e2e repair reinstall");
    placeFile(MACHINE_C, NOTE, await installNote(request, code.token, false));
    stopMachine(MACHINE_C);
    startMachine(MACHINE_C);

    await expect.poll(() => dataFolder(MACHINE_C).includes(NOTE), { timeout: 30_000 }).toBe(false);
    await expect.poll(async () => (await deviceById(request, machine.id))?.status, { timeout: 30_000 }).toBe("online");
    expect(await joinCodeUses(request, code.id), "the code was never spent").toBe(0);
    expect((await devices(request)).filter((d) => d.hostname === MACHINE_C)).toHaveLength(1);
  });

  test("a power cut right after a fresh reinstall saves its identity leaves one new device", async ({ request }) => {
    await liveMachine(request);
    const code = await mintJoinCode(request, "e2e fresh reinstall cut short");
    stopMachine(MACHINE_C);
    placeFile(MACHINE_C, NOTE, await installNote(request, code.token, true));
    const known = await listedIds(request);
    startMachine(MACHINE_C);

    await expect.poll(() => !dataFolder(MACHINE_C).includes(NOTE), { intervals: [50], timeout: 30_000 }).toBe(true);
    cutPower(MACHINE_C);
    startMachine(MACHINE_C);

    const reinstalled = await cameOnline(request, known);
    await expect.poll(async () => (await deviceById(request, reinstalled.id))?.status, { timeout: 15_000 }).toBe("online");
    const created = await newDevices(request, known);
    expect(created.map((d) => d.id), "exactly one new device").toEqual([reinstalled.id]);
    expect(await joinCodeUses(request, code.id)).toBe(1);
  });

  test("a reinstall over a half-finished removal comes back as a new device", async ({ request }) => {
    const machine = await liveMachine(request);
    const code = await mintJoinCode(request, "e2e reinstall over a removal");
    stopMachine(MACHINE_C);
    // A power cut mid-removal leaves an empty folder holding the marker.
    wipeDataFolder(MACHINE_C);
    placeFile(MACHINE_C, "removed", "");
    placeFile(MACHINE_C, NOTE, await installNote(request, code.token, false));
    const known = await listedIds(request);
    startMachine(MACHINE_C);

    const reinstalled = await cameOnline(request, known);
    expect(reinstalled.id).not.toBe(machine.id);
    expect(dataFolder(MACHINE_C)).not.toContain("removed");
  });

  test("two machines sharing one identity are shown to the technician", async ({ authedPage, request }) => {
    const machine = await liveMachine(request);
    stopMachine(MACHINE_C);
    copyDataFolder(MACHINE_C, MACHINE_C_COPY);
    startMachine(MACHINE_C);
    startMachine(MACHINE_C_COPY);

    await authedPage.goto(`/devices/${machine.id}`);
    await expect(async () => {
      await authedPage.reload();
      await expect(authedPage.getByText(/This identity is in use on two machines/)).toBeVisible({ timeout: 2_000 });
    }).toPass({ timeout: 60_000 });
    const listed = (await devices(request)).filter((d) => d.id === machine.id || d.hostname === MACHINE_C_COPY);
    expect(listed.map((d) => d.id), "the console still lists one device").toEqual([machine.id]);
  });
});
