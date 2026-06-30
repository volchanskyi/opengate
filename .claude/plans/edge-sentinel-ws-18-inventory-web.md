# WS-18 — Inventory web view (discovered footprint)

**Objective:** Render a device's discovered footprint (ports, services, DB engines, containers,
packages) from the WS-17 API on the device-detail page, and a freshly-enrolled-machine summary so a
newly onboarded host is instantly legible.

**Dependencies:** WS-6 (device-detail view), WS-17 (inventory API). **Wave:** with WS-12.

## Context

Netdata "builds an interactive diagnostic interface within 60 seconds" of install via auto-discovery.
OpenGate's NAT-safe analogue: the server-stored inventory (WS-17) renders in the central UI. The
ambitious **generative dashboards** (panels that auto-appear per discovered component) are
**deferred** — this WS renders the inventory *data* in a clear, sortable view.

## File inventory

- **Create:** a device-detail **inventory view** — grouped, sortable tables for ports / services /
  DB engines / containers / packages; empty/error states.
- **Modify:** [`DeviceList.tsx`](../../web/src/features/devices/DeviceList.tsx) — a lightweight
  "discovered: N services / M containers" hint on the grid (no high-cardinality content).
- **Reuse:** the generated TS API types from WS-17 (`npm run generate:api`).

## Steps (TDD-first)

1. **Test first:** inventory view component test — fetches WS-17, renders grouped tables, sorts,
   empty/error states → implement.
2. **Test first:** grid hint test — shows counts without leaking detail → implement.
3. `make e2e`: enroll → device detail shows discovered ports/services/containers.

## Gotchas / constraints

- Render only server-returned data (already tenant-scoped + secret-free by WS-16/WS-17).
- Keep it within the existing bundle budget; no new heavy dep (tables, not charts).
- Generative auto-dashboards explicitly out of scope (future WS/ADR if pursued).

## Reviewer checklist

- [ ] Inventory view renders grouped/sortable; empty/error states; grid hint leaks no detail.
- [ ] Uses generated API types; within bundle budget; no new heavy dep.
- [ ] `make e2e` green; `/precommit` green.

## Verification

`cd web && npm test`; `npm run size`; `make e2e` (enroll → inventory view). `/precommit` green.
`/docs`: Devices/Inventory page.
