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

The agent reads host log sources — the systemd journal on Linux and the Windows
Event Log — to compute the log-rate signal
([ADR-048](ADR-048-edge-sentinel-endpoint-log-model.md)) and to serve on-demand
raw pulls ([ADR-046](ADR-046-edge-sentinel-raw-log-broker.md)). Both sources
have native library bindings, but the obvious Linux one is a licensing hazard:
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
- **Windows (Event Log):** shell out to PowerShell `Get-WinEvent -MaxEvents
  <cap> | Select-Object … | ConvertTo-Json` and parse the JSON. Windows event
  levels map to the same normalized level labels; the `Get-WinEvent` cmdlet
  ships with Windows.
- **Agent self-logs:** parse the agent's own `tracing-appender` rotated files
  directly — no external tool.

Every collector is **bounded** (a hard line cap per read) and **no-ops off its
platform** — a missing binary, a non-matching OS, or a non-zero exit yields an
empty result — so a single call site is safe on every fleet machine without
platform branches. The readers are default-off.

## Consequences

- The agent stays pure-Rust and Apache-2-clean: no `libsystemd` link, no GPL
  transitive dependency, and no per-platform native build wrinkle.
- The cost is a subprocess per collection and tolerance for the tools' output
  formats. Both are captured as fixtures in the parser tests, so CI exercises
  the readers without needing a live journal or Event Log.
- Output-format drift (a `journalctl`/`Get-WinEvent` schema change) is a parser
  concern caught by the fixture tests, not a linkage/ABI concern.
- Reader overhead on real Linux and Windows hosts is measured before default-on;
  the parse/fold hot path is tracked in the Edge-Sentinel Criterion bench.
