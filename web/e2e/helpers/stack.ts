/// <reference types="node" />
import { execFileSync } from "node:child_process";
import type { Page } from "@playwright/test";

// The compose stack's routers and machines, driven from outside the programs they carry.

/** The simulated routers: the office's in front of agent-a, the home's in front of a browser. */
export type RouterName = "office" | "home";

/**
 * Changes a router's rules, as `router.sh` takes them: `nat keep|scramble`,
 * `udp open|blocked|helper-only`, `shape MBIT RTT_MS`, `shape off`, `reset`.
 */
export function router(name: RouterName, ...args: string[]): void {
  execFileSync("docker", ["exec", `opengate-test-router-${name}`, "router.sh", ...args], { stdio: "pipe" });
}

/** Puts both routers back to translating, passing UDP and holding no rate. */
export function resetRouters(): void {
  router("office", "reset");
  router("home", "reset");
}

/** Runs a shell command on one of the stack's machines, as its agent's user or as root. */
export function onMachine(machine: string, command: string, { asRoot = false } = {}): string {
  const user = asRoot ? ["-u", "0"] : [];
  return execFileSync("docker", ["exec", ...user, `opengate-test-${machine}`, "sh", "-c", command], { encoding: "utf8" });
}

/** The server's count of sessions whose direct-link attempt ended each way. */
export async function outcomeCounts(): Promise<Record<string, number>> {
  const text = await (await fetch("http://localhost:8081/metrics")).text();
  const counts: Record<string, number> = {};
  for (const match of text.matchAll(/^opengate_direct_link_outcomes_total\{outcome="(\w+)"\} (\d+)$/gm)) {
    counts[match[1]!] = Number(match[2]);
  }
  return counts;
}

/** Notes, inside the tab, when each key goes down and when the terminal's text next changes. */
export async function watchEcho(tab: Page): Promise<void> {
  await tab.evaluate(() => {
    const rows = document.querySelector(".xterm-rows")!;
    const w = window as unknown as { changes: number[]; keyAt: number };
    w.changes = [];
    let last = rows.textContent;
    new MutationObserver(() => {
      if (rows.textContent !== last) {
        last = rows.textContent;
        w.changes.push(performance.now());
      }
    }).observe(rows, { subtree: true, childList: true, characterData: true });
    document.addEventListener("keydown", () => { w.keyAt = performance.now(); }, true);
  });
}

/** Types one character at a time and returns how long each took to appear, in milliseconds. */
export async function echoTimes(tab: Page, count: number): Promise<number[]> {
  const times: number[] = [];
  await tab.locator('[data-testid="terminal-container"]').click();
  for (let i = 0; i < count; i++) {
    // A shorter line keeps the terminal from wrapping, which redraws more than the echo.
    if (i % 40 === 0) {
      await tab.keyboard.press("Control+U");
      await tab.waitForTimeout(1_000);
    }
    const before = await tab.evaluate(() => (window as unknown as { changes: number[] }).changes.length);
    await tab.keyboard.press("a");
    await tab.waitForFunction((n) => (window as unknown as { changes: number[] }).changes.length > n, before, {
      polling: "raf",
    });
    times.push(
      await tab.evaluate(() => {
        const w = window as unknown as { changes: number[]; keyAt: number };
        return w.changes.find((t) => t > w.keyAt)! - w.keyAt;
      }),
    );
    await tab.waitForTimeout(150 + Math.random() * 150);
  }
  await tab.keyboard.press("Control+U");
  return times;
}

/** The value below which `share` of the times fall. */
export function percentile(times: number[], share: number): number {
  const sorted = [...times].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.ceil(share * sorted.length) - 1)]!;
}
