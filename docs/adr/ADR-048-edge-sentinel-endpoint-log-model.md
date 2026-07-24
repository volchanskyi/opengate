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

A conventional central log lake (ship every raw line into Loki) is rejected: it
would put secret-dense text in a shared store, drive storage and I/O on the
capped node, and duplicate — more weakly — what each host's own log source
already retains and can time-query.

## Decision

Adopt an **edge-first, server-proxied** endpoint-log model. Raw log lines are the
signal; they are never bulk-ingested centrally.

- **Raw lines stay at the edge.** The host log source (journald, the Windows
  Event Log, the agent's rotated files) is the durable, time-queryable store of
  record. The server never bulk-ingests raw lines.
- **Access is server-proxied and on-demand.** An operator pulls a bounded,
  redacted, audited window through the transient broker in
  [ADR-046](ADR-046-edge-sentinel-raw-log-broker.md); nothing raw is persisted
  centrally. The browser surfaces this as two panes — Agent Logs (the agent's own
  files) and System Logs (the platform host log, `source=host`) — each with
  severity/time/search filters, and System Logs additionally with a unit filter
  and an enumerated unit dropdown.

## Consequences

- Central storage growth from endpoint logs is zero — logs never leave the edge
  except on an audited, redacted on-demand pull, with no Loki dependency.
- Raw logs are visible only while a device is online. This is the accepted trade
  of the edge-first design; the host's own log store holds the history.
- The hard privacy boundary is the raw pull's audit + elevated-permission +
  redaction controls ([ADR-049](ADR-049-edge-sentinel-raw-log-privacy.md)); the
  host log source is read through first-party CLIs, not a GPL journal library
  ([ADR-050](ADR-050-edge-sentinel-log-reader-sourcing.md)).
- The numeric telemetry channel (`opengate_edge_metric_avg`) carries live host
  resource metrics ([ADR-057](ADR-057-live-host-metric-streaming-and-system-logs.md)),
  not a log-derived signal.
