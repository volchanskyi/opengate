---
number: 130
title: Presence is written on change, last-seen in bulk
---

# ADR-130 — Presence is written on change, last-seen in bulk

## Context

Every heartbeat wrote the machine's status and last-seen time to its database
row, once a minute per machine, and a failed status write ended the connection,
taking any remote session on it down too. At 5,000 machines that is a steady
stream of writes saying nothing had changed, and a short database outage dropped
connected machines for a reason they had no part in.

## Decision

**The connection is presence; the database records changes.** A machine is
online while the server holds its connection. Registration writes online with
the machine's row
([`conn_register.go`](../../server/internal/agentapi/conn_register.go)), and
disconnect writes offline with last-seen at the moment it left. A heartbeat
writes nothing to the database.

**Last-seen is refreshed in bulk.** A sweep on the server's background schedule
([`background.go`](../../server/internal/app/background.go), `PresenceSweep`)
takes the connected machines by tenant and runs one statement per tenant
([`presence_sweep.go`](../../server/internal/agentapi/presence_sweep.go)): it
refreshes last-seen for every connected machine and marks offline every row
still saying online with no connection behind it. A row whose last-seen is
inside a minute's grace is left alone, so a machine that connected after the
sweep took its list is not shown offline until the next one.

**The sweep remembers every tenant it has seen.** It reads each tenant with a
connected machine and each tenant seen since the server started, so a customer
whose last machine left while its offline write failed is still reconciled.

**A failed status write never ends a connection.** It is counted in
`opengate_presence_write_failures_total` and logged; the next sweep corrects
the row. A failed registration still ends the connection, since the row is what
makes a machine exist for the product. The start-up reset marks every row
offline before machines reconnect, which bounds staleness after a server crash.

Kubernetes moved node heartbeats to a small lease record to spare its store at
5,000 nodes; Nomad keeps heartbeat timers in memory and writes status only on a
change; Zabbix writes host availability on a change. Working status out from
last-seen, as Tactical RMM does, and keeping the per-heartbeat write were not
taken.

## Consequences

A connected machine's last-seen is at most one sweep old; a disconnected
machine's is exact. The sweep's outcomes and the rows it takes offline are
counted in `opengate_presence_sweeps_total` and
`opengate_presence_marked_offline_total`, with no device or tenant label
([ADR-076](ADR-076-platform-metrics.md)).
