import { test, expect } from "./fixtures";
import type { Page, WebSocket, WebSocketRoute } from "@playwright/test";
import { openFolder, openWithWorker, savedChecksum, sourceFile, startDownload } from "./helpers/downloads";
import { enrolledMachine, MACHINE_A, MACHINE_B } from "./helpers/enrolled-machine";
import { decodeControlFrame, encodeControlFrame } from "./helpers/frames";
import { endHostSession, sessionPath } from "./helpers/session-tab";
import { onMachine, resetRouters, router } from "./helpers/stack";

// Downloads stream through the service worker into the browser's Downloads, one at a time.
test.use({ serviceWorkers: "allow" });

const BIG_FOLDER = "/tmp/og-big";
const BIG_FOLDER_ENTRIES = 120_000;
const LISTING_PAGE_CAP = 16 * 1024;

test.describe("Downloads", () => {
  test.afterEach(async ({ request }) => {
    resetRouters();
    for (const name of [MACHINE_A, MACHINE_B]) {
      await endHostSession(request, (await enrolledMachine(request, name)).id);
    }
  });

  for (const path of [
    { label: "Direct", machine: MACHINE_B, route: () => {} },
    { label: "Through server", machine: MACHINE_A, route: () => router("office", "udp", "helper-only") },
  ]) {
    test(`a download ${path.label === "Direct" ? "over Direct" : "through the server"} is saved whole`, async ({
      authedPage,
      request,
    }) => {
      const source = sourceFile(path.machine, "og-48.bin", 48);
      path.route();
      const tab = await openWithWorker(authedPage, (await enrolledMachine(request, path.machine)).id);
      await expect(sessionPath(tab)).toHaveText(path.label, { timeout: 10_000 });
      await openFolder(tab, "/tmp");

      const saved = await startDownload(tab, "og-48.bin");
      expect(saved.suggestedFilename()).toBe("og-48.bin");
      expect(await savedChecksum(saved)).toBe(source);
    });
  }

  test("a file the host cannot read ends with a message", async ({ authedPage, request }) => {
    onMachine(MACHINE_B, "echo secret > /tmp/og-locked.txt && chmod 600 /tmp/og-locked.txt", { asRoot: true });
    const tab = await openWithWorker(authedPage, (await enrolledMachine(request, MACHINE_B)).id);
    await openFolder(tab, "/tmp");

    await tab.getByRole("button", { name: "Download og-locked.txt" }).click();
    await expect(tab.getByRole("alert")).toContainText(/Couldn't download og-locked\.txt: .*permission denied/i);
    await expect(tab.getByRole("button", { name: "Download og-locked.txt" })).toBeEnabled();
  });

  test("a second download and a View asked for while one runs wait their turn, then complete", async ({
    authedPage,
    request,
  }) => {
    test.setTimeout(90_000);
    const first = sourceFile(MACHINE_A, "og-24.bin", 24);
    const second = sourceFile(MACHINE_A, "og-2.bin", 2);
    onMachine(MACHINE_A, "echo opengate-viewed-note > /tmp/og-note.txt");
    // A shaped line keeps the first download running while the others are asked for.
    router("office", "shape", "20", "40");
    const tab = await openWithWorker(authedPage, (await enrolledMachine(request, MACHINE_A)).id);
    await expect(sessionPath(tab)).toHaveText("Direct", { timeout: 10_000 });
    await openFolder(tab, "/tmp");

    const firstSaved = await startDownload(tab, "og-24.bin");
    const secondSaved = tab.waitForEvent("download", { timeout: 60_000 });
    await tab.getByRole("button", { name: "Download og-2.bin" }).click();
    await tab.getByRole("button", { name: "View og-note.txt" }).click();
    const secondRow = tab.getByRole("row").filter({ hasText: "og-2.bin" });
    const noteRow = tab.getByRole("row").filter({ hasText: "og-note.txt" });
    await expect(secondRow.getByText("waiting")).toBeVisible();
    await expect(noteRow.getByText("waiting")).toBeVisible();

    expect(await savedChecksum(firstSaved)).toBe(first);
    expect(await savedChecksum(await secondSaved)).toBe(second);
    await expect(tab.getByText("opengate-viewed-note")).toBeVisible({ timeout: 30_000 });
  });

  test("a second download reaching the host while one runs is refused, and the first completes", async ({
    authedPage,
    request,
  }) => {
    test.setTimeout(90_000);
    const first = sourceFile(MACHINE_A, "og-24.bin", 24);
    sourceFile(MACHINE_A, "og-2.bin", 2);
    router("office", "udp", "helper-only");
    router("office", "shape", "20", "40");

    // The page asks for one download at a time, so the second request is put on the relay by hand.
    let toHost: WebSocketRoute | null = null;
    await authedPage.context().routeWebSocket(
      (url: URL) => url.pathname.includes("/relay"),
      (page: WebSocketRoute) => {
        const host = page.connectToServer();
        page.onMessage((m) => host.send(m));
        host.onMessage((m) => page.send(m));
        toHost = host;
      },
    );
    const tab = await openWithWorker(authedPage, (await enrolledMachine(request, MACHINE_A)).id);
    await expect(tab.getByLabel("Through server: no direct route on this network")).toBeVisible({ timeout: 12_000 });
    await openFolder(tab, "/tmp");

    const firstSaved = await startDownload(tab, "og-24.bin");
    await expect.poll(() => toHost).not.toBeNull();
    toHost!.send(encodeControlFrame({ type: "FileDownloadRequest", path: "/tmp/og-2.bin" }));

    await expect(tab.getByRole("alert")).toContainText("Couldn't download og-2.bin");
    expect(await savedChecksum(firstSaved)).toBe(first);
  });

  test("the link lost during a download moves the session to the relay with nothing lost", async ({
    authedPage,
    request,
  }) => {
    test.setTimeout(90_000);
    const source = sourceFile(MACHINE_A, "og-32.bin", 32);
    router("office", "shape", "20", "40");
    const tab = await openWithWorker(authedPage, (await enrolledMachine(request, MACHINE_A)).id);
    await expect(sessionPath(tab)).toHaveText("Direct", { timeout: 10_000 });
    await openFolder(tab, "/tmp");
    const saved = await startDownload(tab, "og-32.bin");

    await tab.getByRole("tab", { name: "Terminal" }).click();
    await tab.locator('[data-testid="terminal-container"]').click();
    await tab.waitForTimeout(2_000);
    router("office", "udp", "helper-only");
    await tab.keyboard.type("echo opengate-");
    await expect(sessionPath(tab)).toHaveText("Through server", { timeout: 6_000 });
    await tab.keyboard.type("$((6*7))-lost-link\n");

    await expect(tab.locator(".xterm-rows")).toContainText("opengate-42-lost-link", { timeout: 15_000 });
    expect(await savedChecksum(saved)).toBe(source);
  });

  for (const path of [
    { label: "Direct", route: () => {} },
    { label: "Through server", route: () => router("office", "udp", "helper-only") },
  ]) {
    test(`a ${BIG_FOLDER_ENTRIES}-entry folder lists in full ${path.label === "Direct" ? "over Direct" : "through the server"}, and the session carries on`, async ({
      authedPage,
      request,
    }) => {
      test.setTimeout(90_000);
      onMachine(
        MACHINE_A,
        `[ -d ${BIG_FOLDER} ] || (mkdir ${BIG_FOLDER} && cd ${BIG_FOLDER} && ` +
          `awk 'BEGIN{for(i=1;i<=${BIG_FOLDER_ENTRIES};i++) printf "entry-%06d.log\\n", i}' | xargs touch)`,
      );
      const pages: number[] = [];
      authedPage.context().on("page", (p: Page) =>
        p.on("websocket", (ws: WebSocket) =>
          ws.on("framereceived", ({ payload }) => {
            if (typeof payload !== "string" && decodeControlFrame(payload)?.type === "FileListResponse") {
              pages.push(payload.length);
            }
          }),
        ),
      );
      path.route();
      const tab = await openWithWorker(authedPage, (await enrolledMachine(request, MACHINE_A)).id);
      await expect(sessionPath(tab)).toHaveText(path.label, { timeout: 10_000 });

      await openFolder(tab, BIG_FOLDER);
      await expect
        .poll(
          async () => {
            const list = tab.getByRole("region", { name: "Folder contents" });
            await list.evaluate((el) => { el.scrollTop = el.scrollHeight; });
            return tab.getByText(`entry-${String(BIG_FOLDER_ENTRIES).padStart(6, "0")}.log`).count();
          },
          { timeout: 60_000 },
        )
        .toBe(1);

      await tab.getByRole("tab", { name: "Terminal" }).click();
      await tab.locator('[data-testid="terminal-container"]').click();
      await tab.keyboard.type("echo opengate-$((6*7))-after-listing\n");
      await expect(tab.locator(".xterm-rows")).toContainText("opengate-42-after-listing", { timeout: 15_000 });
      if (path.label === "Through server") {
        expect(pages.length).toBeGreaterThan(1);
        expect(Math.max(...pages)).toBeLessThanOrEqual(LISTING_PAGE_CAP + 5);
      }
    });
  }
});
