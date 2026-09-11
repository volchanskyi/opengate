---
number: 56
title: Maintenance mode is a desired state on the device row
---

# ADR-056 — Maintenance mode is a desired state on the device row

## Context

Patching a machine produces exactly the readings and events the product is built
to alert on. Without a way to say "this is expected", a maintenance window
produces a page for every machine in it.

## Decision

**Collectors are always on.** There is no flag that turns telemetry off and no
default-on gate to reason about. A machine reports, or it is in maintenance.

**Maintenance is a desired state held by the server**, in columns on the
`devices` row, pushed to the agent over the control channel. The server is the
source of truth, so the state survives an agent restart and is visible to
everyone looking at the fleet.

**Both telemetry and alerting are suppressed**, and the agent stops sampling
rather than sampling and discarding. Collecting readings nobody will look at
costs the customer's processor for nothing.

**The control channel stays connected.** Maintenance is not offline — a
technician can still reach the machine, which is usually why it is in
maintenance.

**On leaving maintenance the sampler re-baselines**, discarding what it learned
during the window. A model trained on a patch run would treat normal operation
as anomalous afterwards.

**Manual only, with no expiry.** The state stays until somebody turns it off. An
automatic expiry would end a window while the work was still going on.

## Consequences

A machine left in maintenance reports nothing, and the fleet view says so
plainly rather than showing it as healthy.
