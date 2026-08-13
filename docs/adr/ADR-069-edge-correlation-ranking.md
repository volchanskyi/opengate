---
adr: 069
title: "Ranking What Broke on the Device, and Retiring the Central Correlation Endpoint"
status: Accepted
date: 2026-08-12
---

# ADR-069: Ranking What Broke on the Device, and Retiring the Central Correlation Endpoint

## Status

Accepted.

## Context

An alert says what crossed a line. The question immediately after it — what else
moved at the same time — was answered centrally: an operator dragged a window on
a device chart, the browser posted it to `/api/v1/devices/{id}/correlate`, and
the server pulled that device's series out of VictoriaMetrics and ranked them.

Two things ended that. The vitals contract
([ADR-065](ADR-065-vitals-contract-cadence-extrema-and-bounded-dims.md)) makes
central telemetry a **60 s average per dimension** at a fixed vocabulary, so a
central ranking would rank averages — and the failures worth ranking are the
ones averaging destroys: a ten-second I/O collapse barely moves the minute it
sits in. And the endpoint was an on-demand telemetry pull, the shape this
program removes everywhere: it made an investigation a query against the
central store, at the moment a fleet is most likely to have several.

Meanwhile the device holds what the ranking actually needs — 1 s readings, in
its own store, for far longer than any central retention window
([ADR-052](ADR-052-edge-sentinel-local-tsdb-build.md)).

## Decision

**The ranking runs on the device, and travels with the alert.** When something
fires on a file server, the agent ranks its own dimensions over the event window
against the stretch immediately before it, and the result rides inside the
alert. The technician who opens the incident already reads `disk.await_ms 0.91,
cpu.total 0.84` — no operator action, no query, and it ranks detail the centre
never had.

**Three signals, blended into one score in `[0, 1]`.** How much a dimension's
distribution changed shape (a two-sample Kolmogorov–Smirnov statistic), how many
readings in the event window fell outside the baseline's normal band, and how
far the mean moved measured against the baseline's own scale. The third earns
its place: the first two saturate on any clean separation regardless of size, so
without it a service time that went from 0.40 ms to 0.44 ms outranks one that
went from 0.4 ms to 40 ms. The weights (0.4 / 0.3 / 0.3) are **behaviour, not
tuning knobs** — changing one invalidates the equivalence the port was proven
against.

**The port was proven against a frozen reference before anything was deleted.**
A fixture of nine dimensions over two windows, one with a deliberately broken
pattern, carries the reference ranking and its scores as committed expected
values
([`correlate_reference.json`](../../agent/crates/mesh-agent-core/tests/fixtures/correlate_reference.json)).
The port reproduces the order, the four numbers to 1e-12, **and the tie-break** —
score, then shape change, then label. Capturing only the primary key would have
passed on the fixture and diverged in production, where dimensions tie routinely
(three of the fixture's dimensions separate completely and differ only in how
far they moved). A blend of doubles is compared within a tolerance rather than
bit-for-bit, because a fused multiply-add is allowed to round once where two
operations round twice.

**Degenerate windows are answered with a number, never a NaN.** A window with
fewer than two readings cannot show a shift, so its dimension is left out rather
than scored from nothing. A gauge that read the same value all hour has no band,
so any different reading counts as anomalous. A baseline of all zeroes has no
scale, so any nonzero mean is a complete shift. A reading that is not a real
number is dropped where it enters the windows — one place — so nothing
downstream has to defend against it, and the ordering can rely on every score
being a real number.

**Every run is bounded three ways**: how many dimensions are examined, how many
readings each window carries, and how long the whole thing may take. The moment
this code runs is the moment the machine is already in trouble, so a rule firing
repeatedly during a storm must never be the reason the machine got worse. The
budget is checked between dimensions, so the first is always scored and a run
always terminates with an answer plus a flag saying it was cut short.

**The read is an MVCC snapshot** of the local store, so a correlation running
while the sampler writes neither blocks ingestion nor sees a moving target.

**`/api/v1/devices/{id}/correlate` is removed outright, not deprecated.** It has
no external consumers, and its only caller — the drag-to-select drill-down on the
device chart — retires in the same change. A deprecated route would keep a
central VictoriaMetrics read path alive for a UI that no longer exists.

**No replacement drag interaction is planned.** The chart's cursor now reads
values and nothing else: a drag that re-scoped the chart would be undone by the
next poll, and the question the drag used to ask is answered before anyone asks
it. Presets remain the way to change the window.

## Consequences

Correlation now costs the centre nothing — no query, no engine, no concurrency
limiter — and runs on hardware the fleet already pays for. It sees 1 s detail
instead of 60 s averages, and it reaches as far back as the device's own store
rather than as far as central retention.

What is lost is asking the question about an arbitrary window of an arbitrary
device on demand. Ranking now exists where an alert exists. A quiet device
nobody alerted on cannot be interrogated this way, which is the accepted trade:
the drill-down was an operator aid used at the moment of an incident, and that
moment is exactly when the alert now carries the answer.

Deleting the central package removes its mutation-test files, so the mutation
shards that carried them are re-pointed rather than left naming a package that
no longer exists.
