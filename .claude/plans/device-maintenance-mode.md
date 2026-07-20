# Device Maintenance Mode + Remove Opt-in Flags — Master Plan

Status: **IN PROGRESS — D1–D9 locked. Implementing directly (user-directed), one workstream at a time. WS-A + WS-B landed 2026-07-17; WS-C landed 2026-07-18; WS-D + WS-E landed 2026-07-19; WS-F pending.**
Owner: Ivan Volchanskyi

## Two coupled changes

### 1. Remove all agent opt-in flags (locked)

Agent capabilities are **always on from the start**. No opt-in, no feature flags.
Delete entirely:

- CLI args + env vars: `--edge-sentinel`/`OPENGATE_EDGE_SENTINEL`,
  `--edge-store`/`OPENGATE_EDGE_STORE`, `--edge-log-readers`/`OPENGATE_EDGE_LOG_READERS`,
  `--edge-discovery`/`OPENGATE_EDGE_DISCOVERY`.
- The `if args.edge_*` spawn gates in `main.rs:464-534` → spawn unconditionally.
- The flag-conditioned capability advertisement in `main.rs:640-656` → always
  advertise `Discovery`, `ThresholdAlerts`, `Backfill`.
- The `default_off_and_opt_in` unit tests (`main.rs:1327-1412`) → replaced by
  "always-on" assertions.
- Any systemd-unit / enrollment plumbing that passed these flags.

**ARM-budget / WS-14 spike gates are dropped, not "reversed"** — with collectors
unconditionally on there is nothing left to gate, so the gates simply cease to
exist. No gate-reversal ADR needed; just remove the gate language wherever it
lives (flag doc comments, edge-sentinel plan, phases).

Open sub-decision: `--edge-store-cap-mb` is a **sizing knob**, not an opt-in.
Keep it (default 512) or fold to a const. Resolve in the agent micro-plan.

### 2. Maintenance mode (the toggle)

A per-device operational state a **system administrator** flips to quiet the
device while they make changes on the host, then flips back:

```
Active  ──enter maintenance──▶  Maintenance  ──exit / auto-expire──▶  Active
(all on)                        (suppressed)                          (all on)
```

## Use-case anchor

A sysadmin is about to do disruptive host work (install/upgrade packages,
restart services, reboot). Without maintenance mode this work would:

- spike metrics and trip the sampler's anomaly detection → **Edge health goes red**;
- churn the Discovered Footprint (services/ports stopping and starting);
- fire **threshold-alert breaches** (WS-19) and page someone;
- pollute the anomaly **baseline** with a state that isn't the real steady state.

Maintenance mode makes the intended disruption **not count**.

## Key model insight: maintenance ≠ offline

The QUIC **control channel stays connected** during maintenance — only telemetry
streams pause. This matters:

- the server can still tell the agent to **exit** maintenance;
- the server distinguishes "in maintenance" from "crashed/offline" (no false
  down-alert for the quiet device);
- remote management may remain usable (see suppression-scope decision).

Architecture reuses the server-authoritative desired-state + control-channel push
pattern (mirrors the existing `PushAlertRules` runtime-reconfigure precedent,
`main.rs:485-501`). Default desired state = **Active**; Maintenance is the
exceptional suppression.

## Decisions

| # | Decision | Choice |
|---|----------|--------|
| D1 | Opt-in flags | **Removed.** All collectors always on; capabilities always advertised. |
| D2 | Toggle semantics | **Maintenance mode** — a temporary suppression, not a telemetry enable. Default Active. |
| D3 | Source of truth | Server-authoritative desired state (Postgres), pushed over the control channel; agent reconciles at runtime. |
| D4 | ARM / spike gates | **Dropped** (moot once always-on). |
| D5 | Suppression scope | **Telemetry + alerting suppressed** (stop metrics/discovery/log collection; suppress anomaly + threshold-alert breaches). **Remote management stays on** (terminal/file/screen) so the admin works through the agent. |
| D6 | Collect vs broadcast | **Stop collecting entirely** — no sampling/discovery/local store writes during the window; leaves a clean, explicit in-maintenance gap that never pollutes the baseline. |
| D7 | Auto-expiry / TTL | **Manual only** — stays until explicitly turned off. No TTL. |
| D8 | Control surface | **Central UI only (v1).** Server is sole source of truth; no local agent CLI. |
| D9 | Authz | **Proposed:** group owner may toggle; every change audited. |

## Proposed behaviors (open sub-decisions, non-blocking)

- **Manual-only safety net (mitigation for D7):** since maintenance never
  auto-reverts, a forgotten device stays blind indefinitely. Compensate with
  **prominent, persistent surfacing** instead of silent suppression: a
  Maintenance badge on the device list, a fleet count of devices-in-maintenance,
  and an escalating "in maintenance for N days" warning. Visibility replaces the
  auto-revert safety net.
- **Baseline reset on exit:** when leaving maintenance, re-baseline anomaly
  detection so the post-change footprint/metrics become the new normal instead of
  alerting on every intended change.
- **External uptime SaaS:** maintenance ideally also silences the external uptime
  monitor for the device — a separate integration, noted not scoped here.
- **UI state:** a distinct "Maintenance" badge; Edge health / Footprint panels
  show "In maintenance since HH:MM" rather than red/anomaly or "No data".

## Workstreams (→ one micro-plan each; finalized after Q1–Q4)

- **WS-A — Flag removal (agent):** delete the four opt-in flags, spawn collectors
  unconditionally, always-advertise capabilities, invert the opt-in tests.
  **Landed 2026-07-17.** Resolved sub-decisions/deviations: `--edge-store-cap-mb`
  **folded to a const** (`EDGE_STORE_CAP_MB`, zero-config); `Backfill` stays gated
  on the local store actually opening (advertising it with no store to drain would
  be unfulfillable) while Discovery + ThresholdAlerts are unconditional; arg
  parsing moved ahead of logging setup. Live developer docs/ADRs/Grafana that
  described the flags as opt-in were reconciled to always-on. **Deferred to WS-F:**
  the broader default-on-gate narrative that is cross-linked by anchor and lives in
  the program-state registers — `phases.md` rows, `techdebt.md` ARM/flip item,
  `decisions.md` index, the Monitoring "Sustained soak and default-on gate"
  heading + `ADR-044`/`Multiscale-Readiness.md` soak references, and archiving
  `edge-sentinel.md`.
- **WS-B — Wire protocol:** `SetMaintenanceMode { enabled }` (server→agent)
  + `MaintenanceApplied { enabled }` agent applied-state report; Rust + Go +
  golden files. **Landed 2026-07-17.** Both variants added to
  `ControlMessage` (Rust `control.rs`, Go `control.go` with an `Enabled *bool`
  so a `false` still serializes); reverse golden `go_control_set_maintenance_mode`
  (Go→Rust) and forward golden `control_maintenance_applied` (Rust→Go), both
  verified by `make golden`. No dispatch/handler wiring yet — the server push and
  agent reconcile land in WS-C/WS-D.
- **WS-C — Server:** migration (`maintenance_on` + `maintenance_since`/`_by`,
  default Active), REST toggle endpoint (authz + audit), control push on connect +
  change, applied-state tracking, fleet count of devices-in-maintenance. No TTL.
  **Landed 2026-07-18.** Resolved sub-decisions/deviations: added a
  `maintenance_reason` column beyond the illustrative list (serves WS-E's
  "toggle with reason" and avoids a second migration); `maintenance_since` is
  stamped only on the Active→Maintenance transition so editing the reason in
  place never resets the entry clock, and the device `Upsert` (re-registration)
  never clobbers operator-set state. The REST toggle
  (`POST /devices/{id}/maintenance`, group-owner authz, audited
  enter/exit) returns **200 even when the agent is offline** — maintenance is a
  desired state, not a live command, so there is no `RestartDevice`-style 409;
  the fleet count is `GET /devices/maintenance-summary` (tenant-scoped, partial
  index). Control push: `SendSetMaintenanceMode` is **ungated** (universal
  control, no capability). The **toggle** pushes unconditionally (enter→true,
  exit→false, so a connected agent is told to resume); the **register reconcile**
  pushes only for a device *currently in maintenance* — Active devices need no
  message because the agent defaults to Active on every fresh registration (keeps
  the common connect path silent and the integration control-stream ordering
  unchanged). The agent's `MaintenanceApplied` is tracked in-memory for
  observability while Postgres stays authoritative. **WS-D contract:** the agent
  MUST reset its applied maintenance state to Active on each registration and
  suppress only when the server pushes `true`. Golden round-trip already landed
  in WS-B; TS types regenerated; the 002-migration rehearsal extended to walk 007
  up/restore/down. The four Device-DTO maintenance fields are **optional /
  omit-zero** (present together only while in maintenance, absent for Active),
  matching the sibling `os_display`/`anomaly_rate` convention — so WS-E reads
  `!!device.maintenance_on` and existing web fixtures stay valid.
- **WS-D — Agent runtime:** on maintenance, **stop** the sampler/discovery/log
  collectors and suppress alert-breach evaluation while **keeping the control
  channel + remote-management** paths live; resume + re-baseline on exit.
  **Landed 2026-07-19.** A shared `MaintenanceGate` (`Arc<AtomicBool>`) in
  `mesh-agent-core::maintenance` is cloned into all three collectors; each
  consults `in_maintenance()` and skips its cycle (sampler: no sample/store
  write/alert eval; discovery: no sweep; log readers: no window). The sampler
  holds a `MaintenanceTransition` and on the maintenance→Active edge re-baselines
  — discards the anomaly ensemble + warm-up and clears breach-emit state so the
  post-change footprint retrains as the new normal. The control loop flips the
  gate on `SetMaintenanceMode`, echoes `MaintenanceApplied { enabled }`, and
  **resets the gate to Active on every registration** (fulfilling the WS-C
  contract: the server pushes `true` post-register only for an in-maintenance
  device). Remote management stays live — `SessionRequest`/logs/hardware/update
  dispatch is untouched. New pure-logic tests in
  `mesh-agent-core/tests/maintenance_test.rs`.
- **WS-E — Web UI:** maintenance toggle (with reason), Maintenance badge on
  device detail + device list, fleet-level in-maintenance count, escalating
  "in maintenance for N days" warning, maintenance-aware empty states (replaces
  the misleading "No data"). **Landed 2026-07-19.** Pure-logic
  `features/devices/maintenance.ts` centralises the escalation model: whole-day
  count clamped at 0, severity bands **normal < 3 d ≤ warn < 7 d ≤ stale**
  (sky → amber → red), plus since/day-label formatters. `MaintenanceBadge` (an
  escalating pill whose colour tracks severity) renders on the list `DeviceCard`
  header and the `DeviceDetail` header; `MaintenancePanel` on device detail does
  enter-with-optional-reason / exit, states since-when + operator reason, and
  raises a day-counting `role="alert"` once past the warn threshold — the visible
  stand-in for the deliberate absence of auto-expiry (D7). Store: `setMaintenance`
  (desired-state POST that succeeds offline, omits an empty reason, updates
  `selectedDevice` **and** the matching list row) + `fetchMaintenanceSummary` /
  `maintenanceCount`. Resolved sub-decisions/deviations: the fleet count is wired
  to the authoritative `GET /devices/maintenance-summary` endpoint (not a
  client-side `devices.filter` derive) as a Dashboard **In Maintenance** tile,
  refreshed on the existing 15 s poll — so the WS-C endpoint is exercised rather
  than left dead. Maintenance-aware empty states: the `DeviceMetrics` edge-health
  panel shows "In maintenance since …" instead of a stale/`No data` health band,
  and both the metrics and Discovered-Footprint empty states say discovery/
  telemetry is paused. The nested-ternary metrics placeholder was extracted to a
  `MetricsPlaceholder` sub-component to keep the changed lines Sonar-clean. No new
  cross-feature exports were needed — the components are `devices`-internal and
  Dashboard reads the already-exported `useDeviceStore`.
- **WS-F — Docs + Edge-Sentinel close-out:** ADR for the maintenance-mode
  operational state + flag removal; decisions.md row; `/docs`. **Closes out the
  Edge-Sentinel program:** removing the flags *is* the default-on flip, so the
  program's only remaining tail is resolved. This WS must:
  - **Skip the ARM footprint bench + sustained soak entirely** — not deferred,
    not post-flip. Per-entity caps keep their current in-code defaults; there is
    no measurement-driven finalization.
  - Delete the default-on gate language wherever it lives (Phase 0 last item,
    Phase 8, WS-15b notes, flag doc comments).
  - Move the Edge-Sentinel row in [`phases.md`](../phases.md) from **In Progress
    → Completed** and **archive `edge-sentinel.md`** (`git mv` to
    `plans/archive/`, bump its internal relative links one `../` deeper, repoint
    the phases Completed row to the `archive/` path) — in this rollout's
    completing commit, so no stale In-Progress master plan is left behind.

## Test posture (TDD)

Failing test first for every source change; no `t.Skip` / `.skip` / `#[ignore]`.
Golden round-trip for the new control message.
