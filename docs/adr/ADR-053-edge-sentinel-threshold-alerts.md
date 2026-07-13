---
adr: 053
title: Edge Sentinel Declarative Threshold Alerts
status: Accepted
date: 2026-07-12
---

# ADR-053: Edge Sentinel Declarative Threshold Alerts

## Status

Accepted.

## Context

Edge-Sentinel already samples host metrics once per second and flags anomalies
with an unsupervised k-means ensemble
([ADR-043](ADR-043-edge-sentinel-local-ml-sampler.md)). Unsupervised anomaly
detection catches *unusual* behavior but not *operator-defined* conditions —
"disk at/above 90 %", "CPU pinned at 95 % for five minutes". Netdata pairs its ML
engine with declarative threshold alarms for exactly this reason.

The design constraints are the same as the rest of Edge-Sentinel: agents are
NAT'd and outbound-only, telemetry ships **default-off** until the soak gate, the
central store keeps `avg` only, and delivery must not become an auto-notify
system before a false-positive-rate soak. A new alert must therefore be
evaluated **at the edge** (a breach is flagged in ~1 s with no central polling
cycle), delivered without a new QUIC stream, and remain investigation-aid only.

## Decision

Add per-tenant declarative threshold rules, evaluated locally beside the ML
anomaly detector.

- **Rule shape.** A rule is a metric selector (a sampler percent gauge:
  `cpu.total`, `mem.used`, `disk.used`), a comparator (`Gt`/`Lt`/`Gte`/`Lte`), a
  fire `threshold`, a hysteresis `clear` boundary, and a `sustain_secs` duration
  ([`ThresholdRule`](../../agent/crates/mesh-protocol/src/control.rs)).
- **Evaluation.** A pure, stateful evaluator
  ([`alerts`](../../agent/crates/mesh-agent-core/src/alerts/)) steps each rule
  Clear → Pending → Firing per sample. A breach must hold **continuously** for
  `sustain_secs` before it fires (rising-edge flap suppression), and once firing
  it stays firing until the metric recovers past the `clear` boundary
  (falling-edge flap suppression). An unrecognized metric or comparator fails
  safe (never fires).
- **Delivery of rules is tenant-scoped and capability-gated.** On registration
  the server pushes the connecting agent's authoritative-org ruleset over a new
  server → agent `PushAlertRules` control message, gated by the new
  `ThresholdAlerts` capability
  ([`alert_rules.go`](../../server/internal/agentapi/alert_rules.go)). The lookup
  key is the resolved org, so one org's rules never reach another; an org without
  a custom set receives a minimal built-in default. Rules are server
  configuration, not a tenant Postgres table — no migration, no new API surface.
- **Breach state reuses `AgentHealthSummary`.** A firing breach rides additively
  in the existing summary (`breaches`), avoiding a new message or QUIC stream and
  respecting the WS-3 payload caps. The server ingests each breach as
  `opengate_edge_alert_breach` scoped to the resolved org, with the `metric`
  label bounded to the known vocabulary and the agent-echoed `rule` id sanitized,
  so a rogue agent cannot drive unbounded label cardinality.
- **Emission is throttled and breach-driven.** The agent emits a breach-carrying
  summary only while a breach is active (plus one final summary reporting the
  clear), throttled above the server's telemetry interval floor, so a steady host
  is silent and a burst never backpressures control.

## Consequences

- Operators get deterministic, low-latency alerts on named conditions without a
  central polling cycle, complementing the ML anomaly signal.
- Delivery is **investigation-aid only** — breaches are signals charted on the
  Edge-Sentinel Soak dashboard, not notifications. Auto-notify waits for the
  false-positive-rate soak, consistent with the master-plan posture. The whole
  path is default-off behind `--edge-sentinel`.
- Because rules live in server configuration keyed by org, per-tenant rule
  management (a UI, a Postgres-backed ruleset, per-user overrides) is an additive
  follow-up: the wire contract, the capability gate, and the tenant-scoped push
  do not change when the rule *source* becomes a table.
- `AgentHealthSummary` is now emitted live for the first time (breach-carrying).
  The anomaly-rate series is written only when a summary actually carries a
  sampler computation, so a breach-only summary does not record a misleading zero
  anomaly rate; a later dense health-summary emission path densifies that series.
