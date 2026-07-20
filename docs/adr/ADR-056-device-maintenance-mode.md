---
adr: 056
title: Device Maintenance Mode and Always-On Edge Collectors
status: Accepted
date: 2026-07-19
---

# ADR-056: Device Maintenance Mode and Always-On Edge Collectors

## Status

Accepted.

## Context

Edge-Sentinel gives every managed device a local sampler, anomaly detector,
threshold-alert evaluator, discovery profiler, and log-rate readers
([ADR-043](ADR-043-edge-sentinel-local-ml-sampler.md),
[ADR-053](ADR-053-edge-sentinel-threshold-alerts.md),
[ADR-050](ADR-050-edge-sentinel-log-reader-sourcing.md),
[ADR-052](ADR-052-edge-sentinel-local-tsdb-build.md)). Two coupled operational
questions were open:

1. **Enablement.** The collectors began behind per-capability opt-in flags while
   their footprint was being characterised, with a planned measurement-gated flip
   to default-on. Carrying opt-in flags indefinitely leaves the product's core
   RMM telemetry off by default on every fleet host and forces enrolment plumbing
   to thread flag state.
2. **Planned disruption.** A system administrator doing disruptive host work
   (package upgrades, service restarts, reboots) spikes metrics, churns the
   discovered footprint, and trips anomaly and threshold-alert breaches. Without a
   way to say "this disruption does not count", the work turns the device red,
   pages someone, and pollutes the anomaly baseline with a state that is not the
   real steady state.

## Decision

### Edge collectors are always-on

Every device runs the full Edge-Sentinel collector set unconditionally — the
sampler/anomaly detector, threshold-alert evaluator, discovery profiler, and
log-rate readers all start with the agent
([`main.rs`](../../agent/crates/mesh-agent/src/main.rs)). There are no opt-in
flags or environment variables and no capability gate on running a collector; the
agent advertises `Discovery` and `ThresholdAlerts` unconditionally, and
`Backfill` whenever its local store opens. The agent-local store cap is a fixed
compile-time constant (`EDGE_STORE_CAP_MB`), a sizing constant rather than an
enablement knob. Per-entity cardinality caps keep their in-code defaults; central
series stay bounded by the avg-only model
([ADR-044](ADR-044-edge-sentinel-server-telemetry-ingest.md)) rather than by any
runtime gate.

### Maintenance mode is a server-authoritative suppression

Maintenance mode is a per-device operational state — `Active` (the default) or
`Maintenance` — that an administrator flips to quiet a device during host work,
then flips back:

- **Source of truth.** The desired state is columns on the `devices` row
  (`maintenance_on` plus `maintenance_since`/`_by`/`_reason`), migration
  [`007_maintenance_mode`](../../server/internal/db/migrations/007_maintenance_mode.up.sql).
  It is pushed to the agent over the QUIC control channel and the agent
  reconciles at runtime — the same server-authoritative desired-state pattern as
  the threshold-alert `PushAlertRules` push.
- **Suppression scope.** Telemetry **and** alerting are suppressed: the sampler
  takes no sample, writes nothing to the local store, and runs no alert
  evaluation; discovery runs no sweep; the log readers open no window. Anomaly and
  threshold breaches therefore cannot fire during the window. **Remote management
  stays live** — terminal, file, screen, hardware, and update dispatch are
  untouched, so the administrator works through the agent while it is quiet.
- **Stop collecting, do not just stop broadcasting.** Nothing is sampled,
  discovered, or written locally during the window, leaving an explicit
  in-maintenance gap that never pollutes the baseline.
- **Control channel stays connected.** Maintenance is not offline: only telemetry
  streams pause. The server can still push an exit, and it distinguishes "in
  maintenance" from "crashed/offline", so a quiet device raises no false
  down-alert.
- **Re-baseline on exit.** On the maintenance → Active edge the sampler discards
  the anomaly ensemble and warm-up and clears breach-emit state, so the
  post-change footprint retrains as the new normal instead of alerting on every
  intended change
  ([`edge_sentinel.rs`](../../agent/crates/mesh-agent/src/edge_sentinel.rs)).
- **Manual only, no TTL.** The state stays until an administrator turns it off.
  There is no auto-expiry.
- **Control surface.** The central web UI is the only control surface; the server
  is the sole source of truth and there is no local agent CLI. The REST toggle
  (`POST /devices/{id}/maintenance`) is group-owner authorised, audits every
  enter/exit, and returns success even when the agent is offline — maintenance is
  a desired state, not a live command. A tenant-scoped
  `GET /devices/maintenance-summary` serves the fleet count of devices in
  maintenance from a partial index.

### Visibility replaces auto-expiry

Because maintenance never auto-reverts, a forgotten device would otherwise stay
blind indefinitely. Prominent, persistent surfacing compensates: an escalating
Maintenance badge on the device list and device detail, a fleet-level
"In Maintenance" count on the dashboard, and a day-counting alert on the device
that escalates the longer a device stays suppressed
([`maintenance.ts`](../../web/src/features/devices/maintenance.ts)). Panels that
would otherwise show red or "No data" instead state that telemetry is paused and
when maintenance began.

## Consequences

- Core RMM telemetry is on for every enrolled device with no flag to set, and
  enrolment carries no collector-enablement state.
- There is no measurement-gated default-on flip and no sustained-soak or
  ARM-footprint gate: the collectors ship on, and the load harness plus ingest
  metrics remain as ongoing observability of the telemetry path
  ([Monitoring](../Monitoring.md)).
- An administrator can quiet a device for planned work without turning it red,
  paging on-call, or corrupting the anomaly baseline, while keeping full remote
  management.
- Silencing the external uptime monitor for a device in maintenance is a separate
  integration and is not wired here.
