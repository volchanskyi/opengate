---
adr: 048
title: Edge Sentinel Endpoint-Log Model (Edge-Stored, Server-Proxied)
status: Accepted
date: 2026-07-08
---

# ADR-048: Edge Sentinel Endpoint-Log Model (Edge-Stored, Server-Proxied)

## Status

Accepted.

## Context

Endpoint logs (journald / syslog, the Windows Event Log, and the agent's own
files) are a core RMM signal, but they are voluminous and secret-dense. The
question is where they live and how the server sees them, under OpenGate's
constraints: agents are outbound-only behind NAT, the control plane runs on one
free-tier OKE node, and cluster block storage is at the 200 GB cap
([ADR-035](ADR-035-oke-free-tier-block-volume-remediation.md)).

Two shapes of log signal exist and must be treated differently:

- **Log-rate signal** — per-level counts, top-emitting-unit ranks, and volume.
  Cheap, bounded-cardinality, and numeric. It carries no message text.
- **Raw log lines** — the free-text records themselves. Secret-dense and
  high-volume.

A conventional central log lake (ship every raw line into Loki) was rejected:
it would put secret-dense text in a shared store, drive storage and I/O on the
capped node, and duplicate — more weakly — what each host's own log source
already retains and can time-query.

## Decision

Adopt an **edge-first, server-proxied** endpoint-log model.

- **Log-rate signal is centralized** in VictoriaMetrics through the scoped
  telemetry client ([ADR-044](ADR-044-edge-sentinel-server-telemetry-ingest.md)),
  as `opengate_edge_metric_avg` samples with `log.rate.<source>.<field>` dims.
  Names carry only level, unit rank, and volume — never a unit name or message
  text — so central cardinality stays bounded and independent of host shape. The
  agent folds a window into the same WS-2 anomaly ensemble that scores host
  metrics.
- **Raw lines stay at the edge.** The host log source (journald, the Windows
  Event Log, the agent's rotated files) is the durable, time-queryable store of
  record. The server never bulk-ingests raw lines.
- **Raw access is server-proxied and on-demand.** An operator pulls a bounded,
  redacted, audited window through the transient broker in
  [ADR-046](ADR-046-edge-sentinel-raw-log-broker.md); nothing raw is persisted
  centrally.

## Consequences

- Central storage growth from logs is bounded by the numeric log-rate series
  count, not raw volume — logs ride the existing VictoriaMetrics volume with no
  new block volume and no Loki dependency for endpoint logs.
- The metrics↔logs correlation win (jump from an anomaly window straight to the
  raw lines for that window) is preserved: the rate signal localizes the window
  centrally, and the raw pull retrieves the lines from the edge on demand.
- Raw logs are visible only while a device is online. This is the accepted
  trade of the edge-first design; the host's own log store holds the history.
- Because the rate signal is numeric and text-free, tenant scoping for it is the
  VM label matcher (application-level org segmentation), while the hard privacy
  boundary is the raw pull's audit + elevated-permission + redaction controls
  ([ADR-049](ADR-049-edge-sentinel-raw-log-privacy.md)).
