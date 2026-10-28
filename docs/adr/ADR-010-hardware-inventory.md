---
number: 10
title: Hardware inventory is its own table, collected on demand
---

# ADR-010 — Hardware inventory is its own table, collected on demand

## Context

Hardware is large, changes rarely, and is unrelated to what a device list needs.
A fleet of hundreds reporting it periodically would spend bandwidth reporting the
same facts.

## Decision

Hardware lives in `device_hardware`, one row per device, cascading on delete.
Three control messages drive it: `RestartAgent`, `RequestHardwareReport` and
`HardwareReport`, the last carrying processor, memory, disk and network
interfaces. It is collected on demand.

`GET /api/v1/devices/{id}/hardware` returns what is stored, or `202` while it
asks. `POST /api/v1/devices/{id}/restart` restarts the agent through exit code
42, which systemd is configured to treat as a restart.

## Consequences

Keeping hardware out of the `devices` row keeps every device query small.
