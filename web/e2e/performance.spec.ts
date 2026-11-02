import type { Page } from "@playwright/test";
import { test, expect } from "./fixtures";
import { openFolder, openWithWorker, savedChecksum, sourceFile, startDownload } from "./helpers/downloads";
import { enrolledMachine, MACHINE_A, MACHINE_B } from "./helpers/enrolled-machine";
import { recordMeasurement } from "./helpers/measurement";
import { endHostSession, msUntilPath, noteRelayOpen, openSessionTab, sessionPath } from "./helpers/session-tab";
import { echoTimes, onMachine, percentile, processorUse, resetRouters, router, sentThrough, watchEcho } from "./helpers/stack";

// The budgets R5 promises, measured on the stack: the office router holds agent-a's line to a rate
// and a 40 ms round trip, and a 120 000-entry folder pages in while a download runs.
test.use({ serviceWorkers: "allow" });

const BIG_FOLDER = "/tmp/og-big";
// The 95th percentile of 120 samples passes its limit only when seven or more of them are slow.
const ECHO_SAMPLES = 120;

// Step 1 measured 288–296 ms and 85–100 ms over Direct, 277 ms and 88 ms through the server.
const ECHO_BUDGET_MS: Record<string, number> = { "10": 350, "100": 100 };
// The share of the shaped rate the download reaches once the listing has paged in.
const RATE_FLOOR = 0.8;
// The rate is read over at least this long, so one dip in the line weighs little against it.
const RATE_WINDOW_S = 30;
// Sized so the download outlasts the echo samples by more than the rate window at either rate.
const FILE_MIB: Record<string, number> = { "10": 128, "100": 1024 };

// Shares of the office line's packets sent out of order: none, the share the link must hold its
// rate through, and a share it is only read at.
const REORDER_PCT = [0, 1, 5];
const REORDER_HELD_PCT = 1;
// Sized so each download outlasts the rate window at a full line.
const REORDER_FILE_MIB: Record<string, number> = { "10": 64, "100": 512 };

// A slow office line, read with a download filling it on both paths; its echo is read, not held.
const SLOW_MBIT = "2";
// Sized so the download outlasts the echo samples at the slow line's full rate.
const SLOW_FILE_MIB = 64;

/**
 * Reads, inside the page, how long the running download takes from now until it holds every
 * piece, and the share it already held at the start.
 */
async function untilDownloaded(tab: Page): Promise<{ seconds: number; share: number }> {
  return tab.evaluate(
    () =>
      new Promise<{ seconds: number; share: number }>((resolve) => {
        const read = () => {
          const bar = document.querySelector('progress[aria-label^="Downloading "]');
          return bar ? Number(bar.getAttribute("value")) / 100 : 1;
        };
        const start = { at: performance.now(), share: read() };
        const timer = setInterval(() => {
          if (read() < 1) return;
          clearInterval(timer);
          resolve({
            seconds: (performance.now() - start.at) / 1000,
            share: start.share,
          });
        }, 20);
      }),
  );
}

/**
 * Reads, inside the page, the running download's share at the start and end of a window of
 * `seconds`, and how long the window took.
 */
async function overWindow(tab: Page, seconds: number): Promise<{ seconds: number; from: number; to: number }> {
  return tab.evaluate(
    (windowMs) =>
      new Promise<{ seconds: number; from: number; to: number }>((resolve) => {
        const read = () => {
          const bar = document.querySelector('progress[aria-label^="Downloading "]');
          return bar ? Number(bar.getAttribute("value")) / 100 : 1;
        };
        const start = { at: performance.now(), share: read() };
        setTimeout(() => {
          resolve({ seconds: (performance.now() - start.at) / 1000, from: start.share, to: read() });
        }, windowMs);
      }),
    seconds * 1000,
  );
}

/** Records under `prefix` the processor the agent and the browser used while the rate was read. */
function recordProcessor(prefix: string, { agent, browser }: { agent: number; browser: number }): void {
  console.log(`${prefix}: agent ${agent.toFixed(2)} cores, browser ${browser.toFixed(2)} cores`);
  recordMeasurement(`${prefix}, the agent's processor`, "read", `${agent.toFixed(2)} cores`, true);
  recordMeasurement(`${prefix}, the browser's processor`, "read", `${browser.toFixed(2)} cores`, true);
}

/** The running download's share received, from the transfers strip, between 0 and 1. */
async function progressOf(tab: Page): Promise<number> {
  const bar = tab.getByRole("progressbar", { name: /^Downloading /, includeHidden: true });
  return (await bar.count()) === 0 ? 1 : Number(await bar.getAttribute("value")) / 100;
}

test.describe("Performance", () => {
  test.beforeAll(() => {
    onMachine(
      MACHINE_A,
      `[ -d ${BIG_FOLDER} ] || (mkdir ${BIG_FOLDER} && cd ${BIG_FOLDER} && ` +
        `awk 'BEGIN{for(i=1;i<=120000;i++) printf "entry-%06d.log\\n", i}' | xargs touch)`,
    );
  });

  test.afterEach(async ({ request }) => {
    resetRouters();
    for (const name of [MACHINE_A, MACHINE_B]) {
      await endHostSession(request, (await enrolledMachine(request, name)).id);
    }
  });

  for (const mbit of ["10", "100"]) {
    for (const path of ["Direct", "Through server"]) {
      test(`typing echoes within ${ECHO_BUDGET_MS[mbit]} ms at ${mbit} Mbit/s ${path === "Direct" ? "over Direct" : "through the server"}, and the download keeps ${RATE_FLOOR * 100}% of the line`, async ({
        authedPage,
        request,
      }) => {
        test.setTimeout(300_000);
        const mib = FILE_MIB[mbit]!;
        const name = `og-${mib}.bin`;
        const source = sourceFile(MACHINE_A, name, mib);
        if (path === "Through server") router("office", "udp", "helper-only");
        router("office", "shape", mbit, "40");

        const tab = await openWithWorker(authedPage, (await enrolledMachine(request, MACHINE_A)).id);
        await expect(sessionPath(tab)).toHaveText(path, { timeout: 10_000 });
        if (path === "Through server") {
          await expect(tab.getByLabel("Through server: no direct route on this network")).toBeVisible({ timeout: 12_000 });
        }
        await openFolder(tab, "/tmp");
        const saved = await startDownload(tab, name);
        await tab.getByRole("button", { name: "og-big", exact: true }).click();

        await tab.getByRole("tab", { name: "Terminal" }).click();
        await watchEcho(tab);
        const times = await echoTimes(tab, ECHO_SAMPLES);
        expect(await progressOf(tab), "the download outlasts the samples").toBeLessThan(1);

        // The rate is the line's: read in the page from here until it holds every piece, before the
        // browser's own saving of the file, which the line does not carry.
        const used = path === "Direct" ? processorUse(MACHINE_A) : null;
        const { seconds, share } = await untilDownloaded(tab);
        const processor = used?.();
        const checksum = await savedChecksum(saved);
        const mbits = ((1 - share) * mib * 8 * 1.048576) / seconds;
        const p95 = percentile(times, 0.95);
        const budget = ECHO_BUDGET_MS[mbit]!;
        const lineShare = mbits / Number(mbit);

        console.log(`${mbit} Mbit/s ${path}: echo p95 ${Math.round(p95)} ms, download ${mbits.toFixed(1)} Mbit/s over ${seconds.toFixed(1)} s`);
        recordMeasurement("echo, 95th percentile", `≤ ${budget} ms`, `${Math.round(p95)} ms`, p95 <= budget);
        recordMeasurement(
          "download, share of the line",
          `≥ ${RATE_FLOOR * 100}%`,
          `${(lineShare * 100).toFixed(1)}% (${mbits.toFixed(1)} Mbit/s)`,
          lineShare >= RATE_FLOOR,
        );
        recordMeasurement("download, time the rate is read over", `≥ ${RATE_WINDOW_S} s`, `${seconds.toFixed(1)} s`, seconds >= RATE_WINDOW_S);
        if (processor) recordProcessor("download", processor);
        expect(p95, `echo times ${times.map(Math.round).join(" ")}`).toBeLessThanOrEqual(budget);
        expect(checksum).toBe(source);
        expect(seconds, "the rate's window").toBeGreaterThanOrEqual(RATE_WINDOW_S);
        expect(lineShare, `${mbits.toFixed(1)} Mbit/s over ${seconds.toFixed(1)} s`).toBeGreaterThanOrEqual(RATE_FLOOR);
      });
    }
  }

  for (const mbit of ["10", "100"]) {
    for (const pct of REORDER_PCT) {
      test(`over Direct at ${mbit} Mbit/s with ${pct}% of the office line's packets out of order, the download's share of the line is read`, async ({
        authedPage,
        request,
      }) => {
        test.setTimeout(300_000);
        const mib = REORDER_FILE_MIB[mbit]!;
        const name = `og-reorder-${mib}.bin`;
        const source = sourceFile(MACHINE_A, name, mib);
        router("office", "shape", mbit, "40", String(pct));

        const tab = await openWithWorker(authedPage, (await enrolledMachine(request, MACHINE_A)).id);
        await expect(sessionPath(tab)).toHaveText("Direct", {
          timeout: 10_000,
        });
        await openFolder(tab, "/tmp");
        sentThrough("office");
        const saved = await startDownload(tab, name);
        const held = pct <= REORDER_HELD_PCT;
        // A share only read is read over the fixed window and the download then stopped, so a slow
        // line measures in about a minute.
        const used = processorUse(MACHINE_A);
        const { seconds, from, to } = held
          ? await untilDownloaded(tab).then(({ seconds, share }) => ({ seconds, from: share, to: 1 }))
          : await overWindow(tab, RATE_WINDOW_S);
        const processor = used();
        const sentBytes = sentThrough("office");
        const checksum = held ? await savedChecksum(saved) : null;
        if (!held) await tab.close();

        const fileBytes = mib * 1024 * 1024;
        const received = (to - from) * fileBytes;
        const mbits = (received * 8) / 1e6 / seconds;
        const lineShare = mbits / Number(mbit);
        const beyond = sentBytes / (held ? fileBytes : received) - 1;
        console.log(
          `${mbit} Mbit/s, ${pct}% reordered: ${mbits.toFixed(1)} Mbit/s over ${seconds.toFixed(1)} s, sent ${(beyond * 100).toFixed(1)}% beyond the file`,
        );
        recordMeasurement(
          `download over Direct at ${mbit} Mbit/s, ${pct}% reordered, share of the line`,
          held ? `≥ ${RATE_FLOOR * 100}%` : "read",
          `${(lineShare * 100).toFixed(1)}% (${mbits.toFixed(1)} Mbit/s)`,
          !held || lineShare >= RATE_FLOOR,
        );
        recordMeasurement(
          `download over Direct at ${mbit} Mbit/s, ${pct}% reordered, sent beyond what arrived`,
          "read",
          `${(beyond * 100).toFixed(1)}%`,
          true,
        );
        recordProcessor(`download over Direct at ${mbit} Mbit/s, ${pct}% reordered`, processor);
        if (held) {
          expect(checksum).toBe(source);
          expect(lineShare, `${mbits.toFixed(1)} Mbit/s over ${seconds.toFixed(1)} s`).toBeGreaterThanOrEqual(
            RATE_FLOOR,
          );
        }
      });
    }
  }

  for (const path of ["Direct", "Through server"]) {
    test(`typing echo at ${SLOW_MBIT} Mbit/s ${path === "Direct" ? "over Direct" : "through the server"} with a download filling the line is read`, async ({
      authedPage,
      request,
    }) => {
      test.setTimeout(300_000);
      const name = `og-slow-${SLOW_FILE_MIB}.bin`;
      sourceFile(MACHINE_A, name, SLOW_FILE_MIB);
      if (path === "Through server") router("office", "udp", "helper-only");
      router("office", "shape", SLOW_MBIT, "40");

      const tab = await openWithWorker(authedPage, (await enrolledMachine(request, MACHINE_A)).id);
      await expect(sessionPath(tab)).toHaveText(path, { timeout: 12_000 });
      await openFolder(tab, "/tmp");
      await startDownload(tab, name);
      await tab.getByRole("tab", { name: "Terminal" }).click();
      await watchEcho(tab);
      const times = await echoTimes(tab, ECHO_SAMPLES);
      const running = (await progressOf(tab)) < 1;
      const p95 = percentile(times, 0.95);

      console.log(`${SLOW_MBIT} Mbit/s ${path}: echo p95 ${Math.round(p95)} ms with the download running: ${running}`);
      recordMeasurement(`echo at ${SLOW_MBIT} Mbit/s ${path}, 95th percentile`, "read", `${Math.round(p95)} ms`, true);
      expect(running, "the download fills the line for every sample").toBe(true);
    });
  }

  test("the session moves to Direct within a second of the relay opening, through the router", async ({
    authedPage,
    request,
  }) => {
    await noteRelayOpen(authedPage.context());
    const tab = await openSessionTab(authedPage, (await enrolledMachine(request, MACHINE_A)).id);
    const ms = await msUntilPath(tab, "Direct", 10_000);
    recordMeasurement("time to Direct after the relay opens", "< 1000 ms", `${Math.round(ms)} ms`, ms < 1_000);
    expect(ms).toBeLessThan(1_000);
  });
});
