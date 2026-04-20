# UI Improvements: Logs Persistence, Token Cleanup, Dashboard Tiles

## Context

Three UX issues on the web client:
1. **Data loss bug** — Agent logs and hardware info reset to empty every 30 seconds because the polling interval calls the same `fetchDevice()` that blanks `hardware` and `logs` on entry.
2. **Missing feature** — No way to bulk-remove expired/exhausted enrollment tokens. Users must delete them one by one.
3. **Dashboard polish** — Tiles are static, monochrome divs. "Total Devices" should link to `/devices`, making "View All Devices" redundant. Tiles need subtle color coding.

---

## Issue 1: Logs & Hardware Reset on Polling

### Root Cause

`fetchDevice()` in `device-store.ts:61-64` resets `selectedDevice`, `hardware`, and `logs` to `null` on every call. The 30s polling interval in `DeviceDetail.tsx:67` calls this same method, destroying user-fetched data.

### Plan

**`web/src/state/device-store.ts`**
- Add `refreshDevice: (id: string) => Promise<void>` to the `DeviceState` interface.
- Implement it: same API call as `fetchDevice` but **no reset**. Pass `false` to `apiAction` for the loading param so no spinner flashes during background polling.
  ```typescript
  refreshDevice: async (id) => {
    const res = await apiAction(set, () =>
      api.GET('/api/v1/devices/{id}', { params: { path: { id } } }), false,
    );
    if (res.ok) set({ selectedDevice: res.data });
  },
  ```

**`web/src/features/devices/DeviceDetail.tsx`**
- Pull `refreshDevice` from the store.
- Change the polling `useEffect` (line 65-69) to call `refreshDevice(id)` instead of `fetchDevice(id)`.
- Keep the initial-load `useEffect` (line 54-62) calling `fetchDevice(id)` — the reset is correct when navigating between devices.

---

## Issue 2: "Cleanup Tokens" Button

### Current State

- `AgentUpdates.tsx` has individual Delete buttons per token.
- No bulk-delete API — only `DELETE /api/v1/enrollment-tokens/{id}`.
- Token status helpers exist in `lib/token-status.ts`: `isTokenExpired()`, `isTokenExhausted()`.

### Plan

**`web/src/state/update-store.ts`**
- Import `isTokenExpired`, `isTokenExhausted` from `../lib/token-status`.
- Add `cleanupInactiveTokens: () => Promise<number>` to the interface.
- Implement: filter tokens for expired/exhausted, call `api.DELETE` for each (not `deleteEnrollmentToken` — that re-fetches after every single delete), do one `fetchEnrollmentTokens()` at the end, return count of deleted.

**`web/src/features/admin/AgentUpdates.tsx`**
- Pull `cleanupInactiveTokens` from the store.
- Compute `inactiveCount` from the token list using the status helpers.
- Add `confirmCleanup` and `cleaningUp` local state.
- Add a "Cleanup Tokens (N)" button next to "Create Token" in the section header. Uses the same two-click confirmation pattern as delete-device elsewhere in the app.
- Only show the button when `inactiveCount > 0`.
- Show toast on completion: "Removed N inactive token(s)".

---

## Issue 3: Clickable Tiles + Color Coding + Remove Redundant Button

### Plan

**`web/src/features/dashboard/Dashboard.tsx`**

Refactor `StatCard` to accept optional `to` and `colorClasses` props:
```typescript
interface StatCardProps {
  label: string;
  value: number | string;
  to?: string;
  colorClasses?: string;
}
```
- When `to` is provided, render as `<Link>` with hover/transition styles.
- When absent, render as `<div>`.

Color scheme (subtle left accent border + faint background tint):
| Tile | Accent | Classes |
|------|--------|---------|
| Total Devices | Blue | `border-l-4 border-l-blue-500 bg-blue-900/10` |
| Online | Green | `border-l-4 border-l-green-500 bg-green-900/10` |
| Device Groups | Indigo | `border-l-4 border-l-indigo-500 bg-indigo-900/10` |
| Offline | Amber | `border-l-4 border-l-amber-500 bg-amber-900/10` |

- Make "Total Devices" tile link to `/devices` via the `to` prop.
- Remove "View All Devices" `<Link>` (now redundant). Keep "Add Device" button.

---

## Implementation Order

1. **Issue 1** — Fixes a real data-loss bug, smallest change.
2. **Issue 3** — Self-contained to one file, purely presentational.
3. **Issue 2** — Touches store + component, needs async bulk-delete logic.

## Files to Modify

- `web/src/state/device-store.ts` — add `refreshDevice`
- `web/src/features/devices/DeviceDetail.tsx` — use `refreshDevice` for polling
- `web/src/features/dashboard/Dashboard.tsx` — refactor `StatCard`, color tiles, remove button
- `web/src/state/update-store.ts` — add `cleanupInactiveTokens`
- `web/src/features/admin/AgentUpdates.tsx` — add cleanup button with confirmation

## Verification

1. `make build` — ensure everything compiles
2. `make test` — run all tests
3. `make lint` — no lint errors
4. Manual browser testing:
   - Navigate to a device, fetch logs, fetch hardware — verify they persist across 30s polls
   - Go to Settings > Agent Settings — verify cleanup button appears only with inactive tokens, requires confirmation, shows toast
   - Dashboard — verify tiles have color accents, "Total Devices" is clickable and routes to `/devices`, "View All Devices" button is gone
