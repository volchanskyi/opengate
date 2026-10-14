/// <reference types="node" />
import { createHash } from "node:crypto";
import type { Download, Page } from "@playwright/test";
import { openSessionTab } from "./session-tab";
import { onMachine } from "./stack";

/** Writes a file of random bytes on the machine once, and returns its checksum. */
export function sourceFile(machine: string, name: string, mib: number): string {
  onMachine(machine, `[ -f /tmp/${name} ] || dd if=/dev/urandom of=/tmp/${name} bs=1M count=${mib} 2>/dev/null`);
  return onMachine(machine, `sha256sum /tmp/${name}`).split(" ")[0]!;
}

/**
 * The checksum of what the browser saved, streamed so a large file stays off the heap; a browser
 * reached through a browser server hands its file over the same way.
 */
export async function savedChecksum(download: Download): Promise<string> {
  const hash = createHash("sha256");
  for await (const piece of await download.createReadStream()) hash.update(piece as Buffer);
  return hash.digest("hex");
}

/** Opens the host's session tab once the service worker controls the device page. */
export async function openWithWorker(page: Page, deviceId: string): Promise<Page> {
  await page.goto(`/devices/${deviceId}`);
  await page.waitForFunction(() => navigator.serviceWorker?.controller != null);
  return openSessionTab(page, deviceId);
}

/** Opens the Files tab and walks into the folder one part at a time. */
export async function openFolder(tab: Page, folder: string): Promise<void> {
  await tab.getByRole("tab", { name: "Files" }).click();
  for (const part of folder.split("/").filter(Boolean)) {
    await tab.getByRole("button", { name: part, exact: true }).click();
  }
}

/** Starts a file's download from its row and returns the browser's download once it begins. */
export async function startDownload(tab: Page, name: string): Promise<Download> {
  const [saved] = await Promise.all([
    tab.waitForEvent("download", { timeout: 30_000 }),
    tab.getByRole("button", { name: `Download ${name}` }).click(),
  ]);
  return saved;
}
