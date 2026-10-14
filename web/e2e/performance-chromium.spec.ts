import { test, expect } from "./fixtures";
import { openFolder, openWithWorker, savedChecksum, sourceFile, startDownload } from "./helpers/downloads";
import { enrolledMachine, MACHINE_A, MACHINE_B } from "./helpers/enrolled-machine";
import { endHostSession, sessionPath } from "./helpers/session-tab";
import { resetRouters, router } from "./helpers/stack";

// The page's own resources during a download, read and frozen through Chromium's debugging
// protocol; playwright.config.ts runs this file in the Chromium project alone.
test.use({ serviceWorkers: "allow" });

test.describe("Performance in Chromium", () => {
  test.afterEach(async ({ request }) => {
    resetRouters();
    for (const name of [MACHINE_A, MACHINE_B]) {
      await endHostSession(request, (await enrolledMachine(request, name)).id);
    }
  });

  test("the page's memory stays flat through a 512 MiB download", async ({ authedPage, request }) => {
    test.setTimeout(180_000);
    const source = sourceFile(MACHINE_B, "og-512.bin", 512);
    const tab = await openWithWorker(authedPage, (await enrolledMachine(request, MACHINE_B)).id);
    await expect(sessionPath(tab)).toHaveText("Direct", { timeout: 10_000 });
    const cdp = await tab.context().newCDPSession(tab);
    const heap = async () => {
      await cdp.send("HeapProfiler.collectGarbage");
      return (await cdp.send("Runtime.getHeapUsage")).usedSize;
    };
    await openFolder(tab, "/tmp");
    const before = await heap();

    const saved = await startDownload(tab, "og-512.bin");
    let peak = before;
    const done = savedChecksum(saved);
    let finished = false;
    void done.finally(() => { finished = true; });
    while (!finished) {
      peak = Math.max(peak, await heap());
      await tab.waitForTimeout(1_000);
    }

    expect(await done).toBe(source);
    expect(peak - before).toBeLessThan(32 * 1024 * 1024);
  });

  test("a tab frozen for 30 s mid-download carries on without loss", async ({ authedPage, request }) => {
    test.setTimeout(150_000);
    const source = sourceFile(MACHINE_A, "og-48.bin", 48);
    router("office", "shape", "20", "40");
    const tab = await openWithWorker(authedPage, (await enrolledMachine(request, MACHINE_A)).id);
    await expect(sessionPath(tab)).toHaveText("Direct", { timeout: 10_000 });
    await openFolder(tab, "/tmp");
    const saved = await startDownload(tab, "og-48.bin");

    await tab.waitForTimeout(3_000);
    const cdp = await tab.context().newCDPSession(tab);
    await cdp.send("Page.setWebLifecycleState", { state: "frozen" });
    await tab.waitForTimeout(30_000);
    await cdp.send("Page.setWebLifecycleState", { state: "active" });

    expect(await savedChecksum(saved)).toBe(source);
    await expect(sessionPath(tab)).toHaveText(/^(Direct|Through server)$/);
  });
});
