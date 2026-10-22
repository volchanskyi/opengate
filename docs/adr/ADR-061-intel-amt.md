---
number: 61
title: Intel AMT is a property of a device
---

# ADR-061 — Intel AMT is a property of a device

## Context

Intel AMT lets a technician power-cycle a machine whose operating system is not
running. It arrives over its own connection, from firmware, with no idea which
managed device it belongs to.

## Decision

**The join key is the SMBIOS system UUID.** The agent reads it from the
machine's firmware tables and reports it; an incoming AMT connection presents
the same value, and the two match on it.

**The key is stored and never returned.** It appears in no API response and no
log. It identifies hardware, and a value that identifies hardware is not a value
to hand out.

**An AMT connection that matches nothing persists nothing.** AMT is a property
of a managed device. A connection from hardware nobody has enrolled has no
tenant, and a row with no tenant has no wall around it.

**Two writers share the hardware row on disjoint columns** — the agent owns what
it discovers, the AMT path owns what it discovers — so neither overwrites the
other.

**`amt_devices` holds connection state only**: the identifier, the device link,
the tenant, and whether it is connected now.

**Only the connection's message loop reads it.** A power request opens a channel
and waits for the loop to route the device's confirmation to it, and the
channel is a byte stream whose delivered data waits in a buffer the size of the
window it advertised ([`mps_channel.go`](../../server/internal/amt/transport/mps_channel.go)),
so a slow or stalled request never holds the loop or its keep-alives, and two
readers never race on one connection. A request's context closes its channel, a
closed channel or connection ends every read with an error, and
[`mps_conservation_test.go`](../../server/tests/integration/mps_conservation_test.go)
holds goroutines and heap flat across requests answered, stalled and never
confirmed.

**The badge follows capability, not connection.** A machine whose hardware can
do this shows the badge whether or not AMT is currently connected, because the
technician's question is "can I power-cycle this" rather than "is it dialled in
right now".

## Consequences

Turning AMT on is done in firmware setup, not in this product, so the
instructions live in the setup pages rather than on the device screen.
