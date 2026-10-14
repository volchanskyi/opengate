/// <reference types="node" />
import { test, expect } from "./fixtures";
import type { WebSocketRoute } from "@playwright/test";
import { enrolledMachine, MACHINE_A } from "./helpers/enrolled-machine";
import { decodeControlFrame, encodeControlFrame } from "./helpers/frames";
import { openSessionTab, stubSessionStart } from "./helpers/session-tab";

// The session start and the relay WebSocket are mocked on the context, so the session tab the
// device page opens decodes real msgpack frames and no session holds the machine.

interface FileEntry {
  name: string;
  is_dir: boolean;
  size: number;
  modified: number;
}

type AuthedPage = Parameters<Parameters<typeof test>[2]>[0]["authedPage"];

async function mockRelay(
  page: AuthedPage,
  listings: Record<string, FileEntry[]>,
  errors: Record<string, string> = {},
): Promise<{ requestedPaths: string[] }> {
  const requestedPaths: string[] = [];

  await stubSessionStart(page.context(), test.info().project.use.baseURL!);
  await page.context().routeWebSocket(
    (url: URL) => url.pathname.includes("/relay"),
    (ws: WebSocketRoute) => {
      ws.onMessage((raw) => {
        if (typeof raw === "string") return;
        const msg = decodeControlFrame(raw);
        if (!msg || msg.type !== "FileListRequest") return;
        const path = msg.path as string;
        requestedPaths.push(path);

        if (path in errors) {
          ws.send(
            encodeControlFrame({ type: "FileListError", path, error: errors[path] }),
          );
          return;
        }
        const entries = listings[path] ?? [];
        ws.send(encodeControlFrame({ type: "FileListResponse", path, entries }));
      });
    },
  );

  return { requestedPaths };
}

async function openFilesTab(page: AuthedPage, deviceID: string) {
  const tab = await openSessionTab(page, deviceID);
  await tab.getByRole("tab", { name: "Files" }).click();
  return tab;
}

test.describe("File Manager flow", () => {
  test("Files tab renders the directory listing from a FileListResponse", async ({
    authedPage,
    request,
  }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    const tracker = await mockRelay(authedPage, {
      "/": [
        { name: "docs", is_dir: true, size: 0, modified: 1_700_000_000 },
        { name: "README.md", is_dir: false, size: 1024, modified: 1_700_000_100 },
      ],
    });

    const tab = await openFilesTab(authedPage, machine.id);

    await expect(tab.getByText("docs")).toBeVisible();
    await expect(tab.getByText("README.md")).toBeVisible();
    expect(tracker.requestedPaths).toContain("/");
  });

  test("clicking a directory navigates and loads the new listing", async ({
    authedPage,
    request,
  }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    const tracker = await mockRelay(authedPage, {
      "/": [{ name: "docs", is_dir: true, size: 0, modified: 1_700_000_000 }],
      "/docs": [
        { name: "guide.txt", is_dir: false, size: 64, modified: 1_700_000_200 },
      ],
    });

    const tab = await openFilesTab(authedPage, machine.id);

    await expect(tab.getByRole("button", { name: "docs" })).toBeVisible();
    await tab.getByRole("button", { name: "docs" }).click();

    await expect(tab.getByText("guide.txt")).toBeVisible();
    await expect(tab.getByText("/docs")).toBeVisible();
    expect(tracker.requestedPaths).toEqual(["/", "/docs"]);

    await tab.getByRole("button", { name: ".." }).click();
    await expect(tab.getByRole("button", { name: "docs" })).toBeVisible();
    expect(tracker.requestedPaths).toEqual(["/", "/docs", "/"]);
  });

  test("permission-denied path renders an error banner", async ({ authedPage, request }) => {
    const machine = await enrolledMachine(request, MACHINE_A);
    await mockRelay(
      authedPage,
      { "/": [{ name: "secret", is_dir: true, size: 0, modified: 1_700_000_000 }] },
      { "/secret": "permission denied" },
    );

    const tab = await openFilesTab(authedPage, machine.id);
    await tab.getByRole("button", { name: "secret" }).click();

    await expect(tab.getByText(/permission denied/i)).toBeVisible();
  });
});
