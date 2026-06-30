# WS-6 — Web UI: core chart engine for high-density device telemetry

**Objective:** Surface anomalies in the React client with a rendering architecture that
survives high series/point density — a health **badge** on the virtualized device grid, a
device-detail **anomaly panel**, **uPlot** metric timelines, **correlation drill-down**
(select window → top-N), and a **fleet-aggregate overview**. Org-scoped via existing auth.
Investigation-aid only (no notifications).

**Dependencies:** WS-5 (correlate endpoint), WS-4 (series + downsampled range endpoint).
**Parallel with:** WS-7.

## Context

The concern is real: React reconciling per-point (SVG/DOM nodes) collapses at ~100
series/device × thousands of points. The fix is architectural, not a library swap —
**React owns chrome; an imperative canvas renderer owns pixels**, fed typed arrays via
refs, never through React state. This is already how the app renders remote desktop
([`use-remote-desktop.ts`](../../web/src/features/remote-desktop/use-remote-desktop.ts) +
[`desktop-worker.ts`](../../web/src/features/remote-desktop/desktop-worker.ts)).

Web is React 19 / TS strict, Vitest + RTL, Tailwind-only, Zustand. The device grid is
virtualized via `@tanstack/react-virtual`
([`DeviceList.tsx`](../../web/src/features/devices/DeviceList.tsx)); routes are already
lazy ([`router.tsx`](../../web/src/router.tsx)); the dashboard polls every 15 s. **No
charting library exists today.**

## Decisions (locked with user)

| Topic | Decision |
|---|---|
| Engine | **Thin adapter over uPlot** (canvas-2D, MIT, v1.6.32) |
| Worst-case load | **Device-detail dense series** + **fleet-aggregate overview**. Grid keeps a **scalar badge** (no sparklines) |
| Updates | **15–30 s polling** (reuse existing pattern); no new streaming transport |
| Scale horizon | **≤500 agents** (verified prod envelope); revisit only at Large-tier |

## Why uPlot (empirical proofs)

- **Size:** 47.9 KB min / ~23 KB gzip (v1.6.32). Ships its own `dist/uPlot.d.ts`
  (no `any`) and `dist/uPlot.min.css` (~1 KB, self-hosted vendor asset like xterm).
- **Perf (README):** "166,650 data points in 25 ms" cold; "~100,000 pts/ms" thereafter;
  streaming 3,600 pts @60fps = **10 % CPU / 12.3 MB RAM** (vs Chart.js 40 %/77 MB, ECharts
  70 %/85 MB). Covers our worst case with large margin.
- **Fit:** canvas-2D, no WASM/eval/network → minimal security surface; CSP already allows
  it with **zero changes** (`script-src 'self'` suffices).

Rejected: Recharts/visx/ECharts (SVG or heavy; bust the budget, don't scale to dense
series). WebGL engine deferred behind the adapter seam — not needed at ≤500 agents.

## Architecture

```
React (chrome): layout, controls, window-select, legend, loading/empty  ── state ──▶ Zustand (metadata only)
        │ mounts once, passes refs
        ▼
TimeSeriesChart adapter  ──imperative──▶  uPlot instance (owns canvas, setData)
        ▲ typed arrays (Float64 x, Float32 y), NEVER React-rendered points
        │
Server (downsampled window)  ◀── poll ──  GET /devices/{id}/metrics?from&to&dims&maxPoints
   Timescale continuous-aggregate pick (10 s raw → 1 min → 1 hr by window)
   + LTTB/min-max decimation so points ≤ maxPoints (≈ chart pixel width)
```

**Core scalability lever:** the *server* guarantees `points ≤ maxPoints` (~1–2 k, the
chart width). The client receives a bounded payload regardless of window span — no
client-side worker/decimation needed at this scale (the desktop-worker pattern is the
escape hatch if profiling later shows a need).

## Data contract (coordinate with WS-4/WS-5)

Column-oriented JSON mapping **1:1 to uPlot's `AlignedData`** (`[xs, ys1, ys2 …]`) so the
adapter does zero reshaping:

```
{ "t": number[],                                  // unix seconds, ascending
  "series": [ { "name": "cpu.util",
               "avg": number[], "min": number[], "max": number[] } ],
  "downsampled": true, "bucket_s": 60 }            // provenance for the legend
```

Adapter builds `Float64Array(t)` + `Float32Array(series.avg/min/max)`, calls
`u.setData(...)`. Per-family charts render **avg line + min–max band** (uPlot bands), so
the 100-series total decomposes into ~4 readable charts (CPU/mem/disk/net), each cheap.
The endpoint, decimation, and continuous-aggregate selection are **server work**
(WS-4/WS-5); WS-6 owns the contract it consumes.

## File inventory

- **Modify:** [`web/package.json`](../../web/package.json) (`uplot`),
  [`web/vite.config.ts`](../../web/vite.config.ts) (`manualChunks: charts`),
  [`web/.size-limit.json`](../../web/.size-limit.json) (entry vs charts budgets),
  [`DeviceDetail.tsx`](../../web/src/features/devices/DeviceDetail.tsx),
  [`DeviceList.tsx`](../../web/src/features/devices/DeviceList.tsx) (badge),
  [`Dashboard.tsx`](../../web/src/features/dashboard/Dashboard.tsx) (overview),
  `web/src/types/api.d.ts` (regen after the metrics endpoint lands in `openapi.yaml`).
- **Create:** `TimeSeriesChart` adapter (the only module importing `uplot`; imperative
  wrapper — create in `useLayoutEffect`, `setData` on prop change, `setSize` on
  ResizeObserver, destroy on unmount; stable interface so a WebGL backend can swap in) +
  device-detail metrics view + telemetry Zustand slice (metadata only) under
  [`web/src/features/devices/`](../../web/src/features/devices/); vendor CSS import of
  `uplot/dist/uPlot.min.css`.

## Bundle strategy (binding constraint)

[`web/.size-limit.json`](../../web/.size-limit.json) measures `dist/assets/*.js` as a
**summed glob** = 250 KB gzip; app is at **223 KB** (~27 KB headroom). uPlot ~23 KB pushes
the *total* near the cap even though it only loads on the (already-lazy) device-detail
route. Fix the budget to model reality:

1. Add a `manualChunks` rule in [`vite.config.ts`](../../web/vite.config.ts) giving uPlot
   a stable chunk name (`charts`).
2. Restructure `.size-limit.json`: an **initial/entry** budget that **excludes**
   `charts-*.js` (protects first paint) + a **named `charts` chunk budget** (~30 KB) so the
   chart cost is explicit and regression-gated.

This keeps first-paint untouched and makes the chart weight a tracked, bounded line — not a
silent erosion of the app budget.

## Steps (TDD-first)

1. **Test first (Vitest/RTL):** badge renders state from a mocked store (anomalous vs
   healthy) → add the badge to the virtualized cell.
2. **Test first:** anomaly panel renders current rate + last transition → implement.
3. **Test first:** the `TimeSeriesChart` adapter calls `setData` with the right typed-array
   shape given a mocked window payload (mock canvas — precedent
   [`desktop-worker.test.ts`](../../web/src/features/remote-desktop/desktop-worker.test.ts));
   assert no `any`. Then implement the imperative wrapper.
4. **Test first:** drill-down posts the selected window and renders top-N (debounce
   window-select) → implement.
5. Add the API client calls (from regenerated TS types) + the fleet-overview chart on the
   dashboard.

## Non-functional requirements

- **Performance.** Server caps payload at ≤ maxPoints; target <16 ms/frame per chart on
  device-detail with all families. Add a perf assertion (point-count bound) + measure under
  the WS-8 load-test.
- **Security.** Canvas-2D only — no eval/WASM/network in uPlot. Data is org-scoped
  server-side (RLS); client renders only returned data. **Charts plot numeric series
  only** — descriptive process data (name/redacted cmdline) stays in its RLS table and is
  never a chart label/tooltip. Vendor CSS self-hosted (`style-src 'self'`); **no CSP change
  required**.
- **Maintainability.** uPlot isolated to one adapter behind a stable interface (swap seam).
  Strict TS, no `any` (uPlot ships types). Tailwind-only honored (CSS as vendor asset).

## Reviewer checklist

- [ ] Component/adapter tests precede implementation; anomalous + healthy states covered;
      adapter test asserts `setData` typed-array shape, not pixels.
- [ ] uPlot isolated to one adapter; vendor CSS as a vendor asset (no custom CSS file); no `any`.
- [ ] Badge on the virtualized grid (scalar, no sparkline); drill-down debounced;
      investigation-aid only (no notify).
- [ ] `manualChunks: charts` + restructured size-limit; entry budget unchanged; `charts`
      chunk within budget (`npm run size`).
- [ ] Charts plot numeric series only — no unredacted cmdline in labels/tooltips.
- [ ] `make e2e` covers badge + panel + timeline + drill-down.

## Verification

`cd web && npm test`; `npm run size` (size-limit); `make e2e` (full Docker lifecycle —
never bare `npx playwright`). WS-8 load-test records frame time + payload point-count under
a 500-agent window. `/precommit` green. `/docs`: web/UI + Wire-Protocol pages.

## Open items to confirm before/at implementation

1. Exact `maxPoints` default (proposed ~2× chart CSS width, capped ~2 k).
2. Window presets (proposed 1 h / 6 h / 24 h / 7 d) and the continuous-aggregate bucket
   each maps to.
3. Whether the fleet overview lives on the dashboard or a dedicated fleet page.
4. `charts` chunk budget number (proposed 30 KB gzip) — ratified by the first real measured build.
