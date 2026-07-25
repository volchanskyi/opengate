# Device-page UX: System Logs caching/layout + group drag-and-drop

## Goal

Six changes across the device pages, all user-reported:

| # | Item | Area |
|---|---|---|
| 1 | System Logs card spans the full grid width (like Discovered Footprint) | `DeviceDetail.tsx` |
| 2 | System Logs pane collapsed by default (like Discovered Footprint child tables) | `LogExplorer.tsx` |
| 3 | Pager arrows render white, not gray, on the yellow buttons | `LogExplorer.tsx` |
| 4 | Log responses cached per device; no auto-refetch on every device open (kills the recurring 409 "log request already in progress") | `device-store.ts`, `LogExplorer.tsx` |
| 5 | Hardware section on the host card collapsed by default | `DeviceDetail.tsx` |
| 6 | Drag a device card onto a group in the sidebar to move it; onto "Ungrouped" to remove it from its group | `DeviceCard.tsx`, `GroupSidebar.tsx`, `DeviceList.tsx`, `device-store.ts`, server `UpdateDevice` |

## Root cause for #4

`SystemLogs` passed `autoLoadOnMount`, so every mount fired a default-window
pull. `fetchDevice` unconditionally wiped `logs` to `null`, so re-opening the
same device always refetched. Two overlapping pulls for one device hit
`agentapi.ErrLogsBusy` → `GetDeviceLogs409` → the toast the user kept seeing
(the agent broker permits one in-flight log request per device).

Fix (three parts):

1. **Cache keyed by device.** `logsDeviceId: Record<LogPaneSource, string | null>`
   next to `logs`. `fetchDevice` keeps a pane's payload when it already belongs
   to the device being opened, clears it otherwise.
2. **Serialize per device.** A module-level promise chain in `device-store.ts`
   keyed by device id: the Agent Logs and System Logs panes can never issue
   overlapping pulls for the same device, so the client never provokes a 409.
3. **Load once, on first expand.** `autoLoadOnMount` → `autoLoadOnExpand`: the
   pane fetches the default window the first time it is expanded *and* holds no
   cached data. Everything after that is manual (window buttons, filters, or the
   new Refresh button). A correlation `focusWindow` still expands + fetches.

## Server change for #6 (unassign)

`moveDeviceToGroup` rejects any target group that does not exist, so the
all-zeros UUID (the placeholder the web client already treats as "unassigned",
`isUnassignedGroup` in `DeviceDetail.tsx`) could not be used to clear a group.
`PostgresDevices.UpdateGroup` already maps `uuid.Nil` → SQL `NULL` via
`nullableUUID`, and `isGroupOwner(uuid.Nil)` is already true, so the handler
just needs to skip the target-group lookup for `uuid.Nil`. Device ownership is
still verified before the move.

## Steps

1. Tests first (each file's `*.test.tsx` / `*_test.go`).
2. `web/src/features/devices/state/device-store.ts` — per-device log cache,
   per-device serialization, `devices[]` updated on a group move.
3. `web/src/features/devices/LogExplorer.tsx` — default-collapsed prop, collapse
   the whole body, white pager arrows, Refresh button, load-on-first-expand.
4. `web/src/features/devices/SystemLogs.tsx` — pass the new props.
5. `web/src/features/devices/DeviceDetail.tsx` — full-width System Logs card,
   Hardware collapsed by default.
6. `web/src/features/devices/DeviceCard.tsx` — draggable card carrying the
   device id + hostname.
7. `web/src/features/devices/GroupSidebar.tsx` — group rows and an "Ungrouped"
   row as drop targets, with a drag-over highlight and a result toast.
8. `server/internal/api/handlers_device_actions.go` — allow `uuid.Nil` to clear.
9. Docs + `phases.md`, archive this plan in the landing commit.

## Reviewer checklist

- Opening a device page twice issues **zero** log requests the second time.
- Expanding System Logs the first time issues exactly one request.
- No 409 toast from two panes on one device.
- Dropping a device on a group moves it; the card leaves a filtered group view.
- Dropping on "Ungrouped" clears `group_id` (Group ID shows `N/A` on detail).
- Keyboard/`select`-based group move still works (drag-and-drop is additive).
