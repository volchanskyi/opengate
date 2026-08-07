---
adr: 050
title: Edge Sentinel Host Log-Reader Sourcing (No-GPL)
status: Accepted
date: 2026-07-08
---

# ADR-050: Edge Sentinel Host Log-Reader Sourcing (No-GPL)

## Status

Accepted.

## Context

The agent reads the host log source — the systemd journal on Linux — to serve the
System Logs pane and on-demand raw pulls
([ADR-046](ADR-046-edge-sentinel-raw-log-broker.md),
[ADR-048](ADR-048-edge-sentinel-endpoint-log-model.md)). Such sources typically
have native library bindings, but the Linux one is a licensing hazard:
`libsystemd` (the journal C API, and the common Rust `systemd`/`sd-journal`
crates that link it) is **LGPL/GPL**, incompatible with the workspace's Apache-2
license. The master plan locks a clean-room, no-GPL agent.

## Decision

Read host log sources through their **first-party command-line tools**, parsing
structured output — no GPL-licensed library is linked into the agent.

- **Linux (systemd journal):** shell out to `journalctl -o json --no-pager -n
  <cap>` and parse the JSON-lines records. Syslog `PRIORITY` bands map to
  normalized level labels; `__REALTIME_TIMESTAMP` microseconds map to RFC 3339
  UTC. `journalctl` ships with systemd on every target host.
- **Agent self-logs:** parse the agent's own `tracing-appender` rotated files
  directly — no external tool.

Every collector is **bounded** (a hard line cap per read) and degrades to an
empty result where its tool is absent — a missing binary or a non-zero exit — so a
single call site is safe on every fleet machine without platform branches.
Collection is **on-demand** (invoked per System Logs pull), with severity, time,
and unit filters pushed down to the tool to bound the read.

A further platform's reader plugs in here: add a `LogSource` variant, its
first-party-tool collector, and the wire `source` name. The wire vocabulary
already carries names beyond what this agent reads, and resolving a requested
source is where honesty is enforced — a name with no reader on this host is
refused by name and counted, never answered with an empty page or another
source's records.

## Consequences

- The agent stays pure-Rust and Apache-2-clean: no `libsystemd` link, no GPL
  transitive dependency, and no per-platform native build wrinkle.
- The cost is a subprocess per collection and tolerance for the tool's output
  format. It is captured as fixtures in the parser tests, so CI exercises the
  reader without needing a live journal.
- Output-format drift (a `journalctl` schema change) is a parser concern caught
  by the fixture tests, not a linkage/ABI concern.
- Reader overhead on real Linux hosts stays tracked: the per-line redaction hot
  path is benchmarked in the Edge-Sentinel Criterion bench.
