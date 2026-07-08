# WS-12 — Log web UI (explorer + metrics↔logs correlation jump)

**Objective:** A point-and-click logs explorer over the on-demand response, a log-rate sparkline on
device detail, and a "logs for this anomaly window" jump from the WS-6 anomaly panel — the
Netdata-style correlation, server-proxied.

**Dependencies:** WS-6 (uPlot panel + device-detail view), WS-11 (logs endpoint). **Parallel
with:** WS-13.

## Context

WS-6 builds the device-detail anomaly panel + uPlot timelines and the virtualized grid badge
([DeviceList.tsx](../../../web/src/features/devices/DeviceList.tsx)). This WS adds the logs surface and
the cross-link: clicking an anomaly (metric or log-rate) opens the explorer pre-filtered to that
device + timeframe.

## File inventory

- **Create:** a device-detail **logs explorer** — filter chips (level/source/unit/time) + full-text
  search over the WS-11 response; paginated; renders only returned data.
- **Modify:** the WS-6 device-detail view — a **log-rate sparkline** (uPlot, reuses the WS-6
  adapter) + a "view logs for this window" action carrying device + `from/to`.
- **Modify:** [`DeviceList.tsx`](../../../web/src/features/devices/DeviceList.tsx) — optional log-health
  hint on the badge (no high-cardinality content).

## Steps (TDD-first)

1. **Test first:** explorer component test (filters/search call the endpoint with the right params;
   renders returned entries; empty/error states) → implement.
2. **Test first:** correlation-jump test (clicking an anomaly opens the explorer with device + window
   prefilled) → implement the action + routing.
3. **Test first:** rate-sparkline adapter test (typed-array `setData`, mocked canvas) → implement via
   the WS-6 uPlot adapter.
4. `make e2e`: badge hint → panel → "logs for window" → explorer renders.

## Gotchas / constraints

- **Charts/labels plot numeric rate only** — raw message text appears **only** in the explorer body,
  never as a chart label/tooltip; full log content is the audited WS-11 response.
- Reuse the WS-6 adapter + lazy chart chunk + the chart-chunk size budget (no new heavy dep).
- The explorer renders only what the server returns (already redacted/permission-gated by WS-11).

## Reviewer checklist

- [ ] Explorer filters/search hit the WS-11 endpoint; pagination + empty/error states tested.
- [ ] Anomaly→logs jump carries device + window; tested.
- [ ] Rate sparkline via the WS-6 adapter; no new chart dep; chunk budget respected.
- [ ] `make e2e` green; `/precommit` green.

## Verification

`cd web && npm test`; `npm run size`; `make e2e` (badge → panel → logs-for-window → explorer).
`/precommit` green. `/docs`: a UI/logs page.
