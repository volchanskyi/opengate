---
number: 44
title: The server writes telemetry, agents never do
---

# ADR-044 — The server writes telemetry, agents never do

## Context

Numeric readings need a time-series store and process listings need rows with a
tenant on them. Letting agents write to the metrics store directly would mean
giving every machine in every fleet a credential for it.

## Decision

**Agents send; the server writes.** Numeric samples go to VictoriaMetrics
through `server/internal/telemetry`. Agents hold no metrics-store credential.

**The tenant is resolved by the server, from the connection.** After handshake
the control path knows which enrolled device this is and therefore which tenant.
A tenant in the payload is ignored. Reads go through the same package, which
injects the caller's own tenant and rejects a caller-supplied one.

**Readings stream live over `AgentMetricWindow`.** The sampler folds its
one-second samples into a window aligned to 60 seconds and sends one message per
closed window over a bounded channel, drained on the heartbeat. The channel
drops under pressure, so a burst of readings can never hold up a restart, a
session or a heartbeat.

**Live and catch-up produce the same numbers.** The live fold and the
reconnect-backfill roll-up share the window key and the arithmetic, so a reading
that arrived live and a reading filled in afterwards for the same dimension and
minute are equal and land in one series. A test asserts the two agree for every
dimension.

**A reading that could not be taken is absent, not zero.** Backfill leaves the
same gap. A partial window is discarded rather than emitted early.

**A reading is checked on its own.** A value the store refuses — not a number,
or infinite — is dropped alone and counted under `invalid_value`, once per
message; the rest of its message is written, and a catch-up batch holding one is
confirmed with it counted as filtered.

**Every catch-up batch gets exactly one answer.** A grant is permission and a
pace, and holds no place. A batch is written only under a live grant and while a
place is free under the global and per-tenant caps in
[`backfill_scheduler.go`](../../server/internal/agentapi/backfill_scheduler.go),
each place given back when its batch is answered. The answer is
`MetricBackfillAck` when every reading was stored or deliberately filtered, and
`DeferBackfill` with a retry time otherwise — no grant, no free place, a failed
write, or a batch refused for its size or its missing tenant — so a machine
never waits on silence and a finished catch-up holds nothing. Each answer is
counted by kind in `opengate_edge_backfill_answers_total`.

**Catch-up reaches back thirty days, the store's keep-time.** The server's floor,
the agent's and both stores' retention are one value, held equal by
[`catch-up-floor-parity.test.sh`](../../scripts/tests/catch-up-floor-parity.test.sh);
older history is answered and not written.

**Persistence is coalesced per connection.** Every handler appends to one
buffer, flushed as a single write through a single slot when the heartbeat opens
the next cycle, or on a size cap, or at teardown. Before this, four handlers
competed for a four-slot pool and the fourth reading of a cycle was dropped.

## Consequences

Drops are counted by reason and charted, so a regression here is visible rather
than absorbed; the monitoring stack raises no alert on a product reading. The
fleet-health badge reads a ten-minute lookback rather than an instant, so a gap
between low-rate summaries never blanks it.
