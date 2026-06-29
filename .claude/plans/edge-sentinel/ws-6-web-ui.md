# WS-6 — Web UI (health badge + anomaly panel + uPlot timelines + drill-down)

**Objective:** Surface anomalies in the React client: a health badge on the virtualized
device grid, a device-detail anomaly panel, uPlot metric timelines, and a correlation
drill-down (select a window → top-N). Org-scoped via existing auth. Investigation-aid only
(no notifications).

**Dependencies:** WS-5 (correlate endpoint), WS-4 (series). **Parallel with:** WS-7.

## Context

Web is React/TS strict, Vitest + RTL, Tailwind-only, Zustand. The device grid is virtualized
via `@tanstack/react-virtual`
([`DeviceList.tsx`](../../../web/src/features/devices/DeviceList.tsx)); the dashboard polls
every 15 s. **No charting library exists today** — add one. Data comes from the WS-5 REST
endpoint + a series range-query proxy.

## File inventory

- **Modify:** [`web/package.json`](../../../web/package.json) — add `uplot` (canvas; handles
  dense series; Recharts is the fallback if DX is preferred).
- **Modify:** [`web/src/features/devices/DeviceList.tsx`](../../../web/src/features/devices/DeviceList.tsx) — health badge per grid cell.
- **Create:** device-detail metrics view + anomaly panel + uPlot timeline + correlation
  drill-down under [`web/src/features/devices/`](../../../web/src/features/devices/); a small
  Zustand slice for telemetry state.

## Steps (TDD-first)

1. **Test first (Vitest/RTL):** badge renders state from a mocked store (anomalous vs
   healthy) → add the badge to the virtualized cell.
2. **Test first:** anomaly panel renders current rate + last transition → implement.
3. **Test first:** timeline component maps series → uPlot props; drill-down posts the
   selected window and renders top-N → implement (debounce window-select interactions).
4. Add the API client calls (from regenerated TS types).

## Gotchas / constraints

- **Tailwind-only** — uPlot ships a vendor stylesheet; import it as a **vendor asset**, do
  not hand-write CSS files.
- **Strict TS, no `any`.** Debounce drag/select so React state churn doesn't drop frames.
- Org scoping is server-enforced (RLS); the UI must not assume cross-org data.

## Reviewer checklist

- [ ] Component tests precede implementation; anomalous + healthy states covered.
- [ ] uPlot vendor CSS as a vendor asset (no custom CSS file); no `any`.
- [ ] Badge on the virtualized grid; drill-down debounced; investigation-aid only (no notify).
- [ ] `make e2e` covers badge + panel + timeline + drill-down.

## Verification

`cd web && npm test`; `make e2e` (full Docker lifecycle — never bare `npx playwright`).
`/precommit` green. `/docs`: web/UI page.
