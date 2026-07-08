---
adr: 047
title: Web Telemetry Chart Engine — uPlot Adapter + Logs Explorer
status: Accepted
date: 2026-07-08
---

# ADR-047: Web Telemetry Chart Engine — uPlot Adapter + Logs Explorer

## Status

Accepted.

## Context

Edge-Sentinel ingests numeric telemetry to VictoriaMetrics and brokers raw logs
on demand ([ADR-044](ADR-044-edge-sentinel-server-telemetry-ingest.md),
[ADR-046](ADR-046-edge-sentinel-raw-log-broker.md)), and exposes a downsampled
range endpoint plus an on-demand metric-correlation endpoint. The React client
had no way to surface any of it: no charting library existed, and React
reconciling per-point DOM/SVG nodes collapses at ~100 series/device × thousands
of points.

## Decision

Render telemetry through a thin adapter over **uPlot** (canvas-2D), with React
owning chrome and the renderer owning pixels.

- [`TimeSeriesChart`](../../web/src/features/devices/charts/TimeSeriesChart.tsx)
  is the **only** module importing uPlot. It creates the instance in a layout
  effect, pushes data via `setData` (typed arrays — `Float64Array` x,
  `Float32Array` y built by
  [`aligned-data.ts`](../../web/src/features/devices/charts/aligned-data.ts)),
  tracks size via `ResizeObserver`, and destroys on unmount. The interface is
  engine-agnostic so a WebGL backend can swap in behind it.
- uPlot is code-split into a stable `charts` chunk
  ([`vite.config.ts`](../../web/vite.config.ts)) with its own gzip budget so the
  chart weight is regression-gated separately from first paint
  ([`.size-limit.json`](../../web/.size-limit.json)).
- The device-detail panel
  ([`DeviceMetrics`](../../web/src/features/devices/DeviceMetrics.tsx)) renders
  the anomaly rate, per-family timelines, and a drag-to-correlate drill-down. The
  virtualized grid and dashboard carry only **scalar** health badges
  ([`HealthBadge`](../../web/src/features/devices/HealthBadge.tsx),
  [`FleetHealth`](../../web/src/features/devices/FleetHealth.tsx)) — no
  per-device series on the grid.
- **Band provenance is labelled honestly.** Central VM is `avg`-only, so chart
  bands carry a `min_max_source` (`local` / `avg_of_10s` / `none`); the UI never
  implies host extrema the source cannot back.
- The logs explorer
  ([`DeviceLogs`](../../web/src/features/devices/DeviceLogs.tsx)) renders only the
  redacted lines the WS-11 broker returns, with level/time/full-text filters and
  page facets. A log-rate sparkline
  ([`LogRateSparkline`](../../web/src/features/devices/LogRateSparkline.tsx))
  plots only numeric `log.rate.*` dims — message text is never a chart label. A
  metrics↔logs correlation jump carries a device window from the panel into the
  explorer.

## Consequences

- The render path never touches the React reconciler per point, so a 15–30 s
  polling refresh stays cheap at the verified ≤500-agent envelope; a WebGL
  backend or a worker-decimation path is the escape hatch behind the adapter
  seam if a larger tier ever needs it.
- uPlot is canvas-2D with no eval/WASM/network, so the existing CSP needs no
  change; its vendor CSS is self-hosted like the xterm asset.
- Charts plot numeric series only; process basenames and raw log text never
  become a chart label or tooltip, keeping the visual surface free of the
  secret-dense content the broker and RLS tables guard.
- The `charts` chunk loads only on the (already lazy) device-detail route, so
  first paint is unaffected and the budget makes the chart cost explicit.

Working plans:
[edge-sentinel-ws-6-web-ui.md](../../.claude/plans/archive/edge-sentinel-ws-6-web-ui.md),
[edge-sentinel-ws-12-log-web.md](../../.claude/plans/archive/edge-sentinel-ws-12-log-web.md).
