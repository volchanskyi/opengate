---
number: 44
title: The server writes telemetry, agents never do
---

# ADR-044 — The server writes telemetry, agents never do

## Context

Numeric readings need a time-series store and process listings need rows with a
tenant on them. Letting agents write to the metrics store directly would mean
giving every machine in every fleet a credential for it.

Once that was settled, the write path itself became the limit. Each connection
buffered its readings and wrote them through one of four shared slots when its
heartbeat came round, so a store that slowed down held up heartbeats and alerts
behind it, a fleet back from an outage competed with live readings for the same
slots, and nothing told a machine which of its minutes the store had actually
taken. A machine re-sent from fixed positions, a store that crashed lost what it
had accepted and not yet saved, and nobody asked for it again.

## Decision

**Agents send; the server writes.** Numeric samples go to VictoriaMetrics
through `server/internal/telemetry`. Agents hold no metrics-store credential.

**The tenant is resolved by the server, from the connection.** After handshake
the control path knows which enrolled device this is and therefore which tenant.
A tenant in the payload is ignored. Reads go through the same package, which
injects the caller's own tenant and rejects a caller-supplied one.

**One writer, two classes, one sender each.** Every connection hands its
readings to one server-wide writer
([`writer.go`](../../server/internal/telemetry/writer.go)) and returns to its
control loop at once, so a heartbeat or an alert never waits on the store. Live
readings and catch-up batches queue apart, each under a byte bound counted on
the encoded bytes it holds, and each class has a sender of its own, so a live
reading never waits behind a catch-up request
([`DefaultWriterConfig`](../../server/internal/telemetry/writer.go) holds the
bounds, the flush size and age, and the write timeout). Each reading is checked
and encoded once, when it is handed over
([`encode.go`](../../server/internal/telemetry/encode.go)); one the store would
refuse — not a number, infinite, a bad name — is dropped alone and counted, and
the rest of its message is written. The writer's goroutines are fixed whatever
the fleet size. Kubernetes gives each priority of API request seats a lower one
cannot take; one sender taking short catch-up requests in turn moved too few
readings for the catch-up promise.

**Every hand-off is answered once, after the store answers.** A live window the
store accepted is confirmed to the machine with `LiveWindowStored`, sent only to
a machine that advertises `LiveConfirmations`. A catch-up batch gets
`MetricBackfillAck` when every reading was stored or deliberately filtered, and
`DeferBackfill` with a retry time otherwise, within the writer's queue wait plus
its write timeout. A confirmation means accepted by the store, as Prometheus
remote write, vmagent and Datadog define it.

**Delivery is at least once.** A machine re-sends only what was never confirmed
([ADR-052](ADR-052-agent-local-store.md)); a repeat comes from an answer lost to a
cut link or a server fault, a machine crash before its next save, or a re-send
the server asked for. Every chart reads averages, lows and highs, so a repeat
changes none of them. Folding copies together in the store was not taken.

**Catch-up is admitted per batch, by the fill of its own queue.** A grant is
permission and a pace, and holds no place. A batch is taken when the machine holds
a grant, has no other batch in flight, its customer is inside its share, and the
catch-up queue is in normal mode
([`backfill_scheduler.go`](../../server/internal/agentapi/backfill_scheduler.go)).
Past the high mark the queue turns throttled until it drains under the low mark:
turns go to waiting customers in rotation, then to each customer's machines in
rotation, each customer always sending one batch a turn whatever its size, with
an overshoot charged to its later turns, so no number of waiting customers locks
one out. This is the Zabbix model, whose server stops taking proxy uploads at 80%
of its write cache and takes them one at a time until 20%. The same batch arriving
twice while queued joins the first and is written once. Each answer is counted by
kind in `opengate_edge_backfill_answers_total`.

**A lost write is asked for again.** The server reads the store's start time
([`store_watch.go`](../../server/internal/telemetry/store_watch.go)); when it
changes, every connected machine that advertises `ResendRequests` is sent
`DeferBackfill` carrying the time to send again from, spread over a minute, and
a machine offline then is told once at its next connect while the server stays
up ([`resend.go`](../../server/internal/agentapi/resend.go)). When the live queue
drops a machine's readings, the writer notes that machine's first dropped minute,
and once the queue is under its low mark those machines alone get the same
request. A server that restarts together with the store cannot see the change, so
the seconds the store had accepted and not saved stay lost; the
[Device Health](../product/Device-Health.md#offline-machines-lose-nothing) chapter
says so. Netdata's central node asks each machine for what it lacks in the same
way.

**A message is measured before it is read.** The machine connection reads a
frame's declared length first and refuses one longer than the largest message a
machine sends, then reserves it from a reading allowance every connection shares
before allocating for it
([`read_allowance.go`](../../server/internal/agentapi/read_allowance.go)); a
reader past the allowance waits. Go's memory limit sits below the container's
([`memlimit.go`](../../server/internal/app/memlimit.go)), so the collector works
to the limit rather than to twice the live heap, and the allowance is a share of
that limit: a catch-up batch read costs about 3.3 bytes more for each byte while
it is decoded, and with 5,000 machines connected the server holds about 250 MB of
its 346 MiB limit, so a share that keeps a full allowance's four to six bytes a
byte inside the rest grows with the server rather than being chosen for one size.
Kafka bounds the bytes of requests it holds queued the same way. gRPC refuses an oversized
message from its length header the same way. A machine's log answer pages itself
to stay under the ceiling ([ADR-046](ADR-046-logs-stay-on-the-machine.md)).

**Erasure wins over a write in flight.** A write is registered, then its machine
checked against the deny-list, just before the request goes out; the erasure job
writes the deny-list entry, then waits for every write registered before it, and
only then deletes the machine's series
([ADR-054](ADR-054-data-lifecycle.md)).

**Live and catch-up produce the same numbers.** The live fold and the
reconnect-backfill roll-up share the window key and the arithmetic, so a reading
that arrived live and a reading filled in afterwards for the same dimension and
minute are equal and land in one series. A test asserts the two agree for every
dimension. Each machine sends its live window at a second of the minute taken
from its machine id, so a fleet that reconnected together still reaches the
server evenly, as Prometheus offsets each scrape target.

**A reading that could not be taken is absent, not zero.** Backfill leaves the
same gap. A partial window is discarded rather than emitted early, and a minute
that has not ended is never sent.

**Catch-up reaches back thirty days, the store's keep-time.** The server's floor,
the agent's and both stores' retention are one value, held equal by
[`catch-up-floor-parity.test.sh`](../../scripts/tests/catch-up-floor-parity.test.sh);
older history is answered and not written.

## Consequences

The promise the
[catch-up profile](../../load/profiles/catch-up.yaml) gates every night — live
readings in the store within 5 seconds during any catch-up, and 5,000 machines
back from an hour's outage all filled within 15 minutes — is a property of this
path, measured from the server's own receipt-to-acceptance figure.

Drops are counted by reason and charted, so a regression here is visible rather
than absorbed; the monitoring stack raises no alert on a product reading. The
fleet-health badge reads a ten-minute lookback rather than an instant, so a gap
between low-rate summaries never blanks it.

A completed catch-up session gives back what it took, held by
[`catchup_conservation_test.go`](../../server/tests/integration/catchup_conservation_test.go).
