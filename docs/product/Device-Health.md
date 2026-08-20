# Device Health

What OpenGate knows about a machine's condition, and what a technician sees when
they look at it: the fixed vocabulary of readings a device reports, what each one
means, the readings a platform cannot supply, quieting a machine during host work,
and the charts the browser draws from all of it.

Detection built on top of these readings — thresholds, rules, evidence — is in
[Alerts and Rules](./Alerts-and-Rules.md). How the readings are stored, scraped
and paid for centrally is in
[Monitoring](../infrastructure/Monitoring.md).

## What a device reports

The host-resource dimensions an agent reports are listed in
[Wire-Protocol](../architecture/Wire-Protocol.md). Disk is reported per mount and reduced to two
numbers by [`sampler.rs`](../../agent/crates/mesh-agent-core/src/ml/sampler.rs):
**`disk.used_percent` is the fullest mount** and `disk.mounts_critical` counts
how many mounts sit at or above the critical-usage threshold. The fullest mount
is what an operator can act on — a file server with a small system volume at 98 %
beside a 2 TB data volume at 10 % pools to about 15 %, and a threshold rule
watching a pooled figure never fires for the volume that is about to fill.

## The vitals contract

A device writes a fixed vocabulary of central series, and cardinality — not
sample rate — is what bounds the central store. Each of the six host-resource
gauges ships its 60 s average, and the four where a within-minute spike is the
signal (cpu, memory, and both net rates) ship the window maximum beside it: over
a minute at 1 Hz, five seconds pinned at 100 % move a 20 % average to 26.7 %,
while the maximum reads 100. Five stall vitals and three disk-performance vitals
ship beside them, and those eighteen dims plus the node-wide anomaly rate and the
five per-family rates are exactly the 24 series a Linux device may occupy. The
cap is spent: the next vital of any kind is a decision about the cap itself
rather than something that fits quietly.

Two dimensions answer to a second name as well: the aliases `mem.used` and
`disk.used` resolve to `mem.used_percent` and `disk.used_percent`, so a rule
written against either name watches the same reading.

A host that supplies neither Linux-only source writes the sixteen
platform-neutral series and reports the missing eight as unsupported — it ships
no dim for them at all, because a run of zeroes would read as a calm machine
rather than as a machine nobody is measuring.

## Stall vitals

`stall.cpu.some`, `stall.mem.some`, `stall.mem.full`, `stall.io.some` and
`stall.io.full` are each the share of the last 60 s that tasks spent stalled
waiting on that resource, read straight from the kernel's pressure accounting by
[`pressure.rs`](../../agent/crates/mesh-agent-core/src/ml/pressure.rs). The kernel
has already reduced the whole minute into one number, so a stall vital costs one
file read and no cardinality, its averaging window is exactly the cadence it
ships on, and its 60 s bucket carries the latest reading rather than a mean of
sixty overlapping averages. The CPU `full` line is not published: the kernel
defines it as always zero.

An agent running inside a container reads its own cgroup's pressure files rather
than the host's, so it never reports its neighbours' stalls as its own; if that
cgroup publishes none, the vitals are absent rather than filled from the host.

These five ship from a Linux agent. A platform with no time-in-stall measurement
of its own gets no substitute built from counters that measure something else —
that would put two meanings behind one name — so it reports `stall.*` as
unsupported and ships nothing. **An absent stall vital is absent, never zero:**
zero is the answer for a host that was measured and never stalled, which is a
different fact from a host that cannot measure stalling.

## Disk-performance vitals

`disk.used_percent` answers "is the disk full". `disk.await_ms`,
`disk.await_ms.max` and `disk.queue_depth` answer "is it slow", which is a
different question about a different entity — capacity is a property of a mount,
service time of a physical device. [`diskperf.rs`](../../agent/crates/mesh-agent-core/src/ml/diskperf.rs)
derives them from the kernel's per-device I/O counters: average time one I/O
took, its within-minute peak, and how many I/Os were outstanding on average.

**Worst device, per vital, independently.** The device with the slowest service
time and the device with the deepest queue are routinely different — a wearing
system disk beside a data disk taking a backup — and each vital answers its own
question, so a mean across them would describe neither. Per-device detail rides
alert evidence rather than central series, so the reduction costs no cardinality.
The devices measured are the whole block devices the kernel lists, which excludes
partitions by construction, minus the loop, ram and zram pseudo-devices; mapper
and RAID devices are included, because encryption and RAID overhead is latency
the user waits for.

There is deliberately no busy-percentage vital. SSD and NVMe service many I/Os in
parallel, so a utilization percentage pins at 100 % with substantial headroom
left; queue depth keeps scaling where it saturates, which is what distinguishes
"busy but healthy" from "overloaded".

A virtual machine needs no special handling: the latency its guest kernel
measures already includes host contention and volume throttling, which is
precisely what makes the customer's application slow. A **containerized** agent
reports these vitals as unsupported instead — the kernel's disk counters are not
per-container, so a number here would be its neighbours' I/O reported as its own,
and the cgroup's own I/O accounting carries no service time to substitute. What
the container keeps is a genuine I/O-stall signal through `stall.io.*`.

The vocabulary is fixed on both sides: the agent builds it from one series
mapping ([`store_sink.rs`](../../agent/crates/mesh-agent-core/src/ml/store_sink.rs))
and the server allowlists it before writing a label
([`vitals.go`](../../server/internal/agentapi/vitals.go)), so a dim name arriving on
the wire cannot enlarge the store — an unlisted one is dropped and counted under
`opengate_edge_telemetry_drops_total{reason="unknown_dim"}`. The two lists are
pinned together by the cross-language golden fixture for a metric window, and
the per-device cost is measured against a real VictoriaMetrics in
[`spike_test.go`](../../server/tests/vmcardinality/spike_test.go).

## Anomaly state

Beside the vitals a device reports whether its own readings are behaving. The
agent trains a small consensus ensemble of `k=2` models locally over the metric
families it samples and votes each new reading against them
([`ensemble.rs`](../../agent/crates/mesh-agent-core/src/ml/ensemble.rs)) — a
clean-room implementation on the device, so nothing is sent anywhere to make the
judgement. What reaches the centre is the summary: the node-wide anomaly rate,
the same rate per metric family, and a compact bitmask of which of the recent
readings were flagged, which is what lets a chart show *when* a machine started
misbehaving rather than only that it is.

The summary carries the **sampler and model versions** that produced it. Those
are what make two readings comparable: a rate from one model generation and a
rate from the next are two different measurements, and a chart that draws them as
one line invents a change nobody made. The wire fields are in
[Wire Protocol](../architecture/Wire-Protocol.md#control-message-variants).

## Reconnect backfill and deep-history pull

An agent stores its own metric history in the durable local tiers
([`edge-tsdb`](../../agent/crates/edge-tsdb)), so an
offline window loses nothing centrally. On reconnect the agent advertises the
`Backfill` capability and requests a server-coordinated admission slot; the
scheduler ([`backfill_scheduler.go`](../../server/internal/agentapi/backfill_scheduler.go))
grants a rate or defers under live load. Once granted, the agent drives the pure
replay engine ([`backfill`](../../agent/crates/mesh-agent-core/src/ml/backfill/mod.rs))
from the control loop ([`backfill_loop.rs`](../../agent/crates/mesh-agent/src/backfill_loop.rs)):
it drains the recent window first as 60 s points — the same grid the live stream
emits on, so a backfilled point and a live point for the same second are the same
point — then the older 1 min and 1 hr rollups oldest-first, throttled to the
granted rate and one acked batch at a time. Each bucket carries its average and,
for the gauges that have one, its maximum, taken from the stored rollup rather
than recomputed from averages. Full-resolution 1 s raw is never pushed; a durable per-tier watermark
advances only on each `MetricBackfillAck`, so an interrupted drain resumes
without re-sending.

The server writes each batch to the matching VM tier at the sample's original
timestamp through the import API, never the live stream-aggregation pipeline —
stream aggregation buckets by arrival time, so historical rollups would land in
the wrong buckets ([`conn_backfill.go`](../../server/internal/agentapi/conn_backfill.go),
[`spike_test.go`](../../server/tests/vmbackfill/spike_test.go)). Samples are clamped
to VM retention and bounded against wild clocks on both sides. History older than
retention, or full-resolution 1 s detail, is reachable through an admin-gated,
single-host deep-history pull (`GET /devices/{id}/history`) that the server
brokers to the agent as a bounded `RequestLocalHistory`; the agent answers it
from its local T0 raw tier.

## Maintenance mode

An administrator can put a device into **maintenance mode** to quiet it during
disruptive host work — package upgrades, service restarts, reboots — that would
otherwise spike metrics, churn the discovered footprint, and trip anomaly and
threshold-alert breaches. Maintenance is a per-device desired state held on the
server (default Active) and pushed to the agent over the control channel; while it
is set the agent stops sampling, discovery, and log collection and suppresses
alert evaluation, so the intended disruption never counts. Remote management stays
live and the control channel stays connected, so the device is distinguishable
from one that has crashed and the server can push an exit. On leaving maintenance
the agent re-baselines anomaly detection, retraining the post-change footprint as
the new normal. Maintenance is manual-only with no auto-expiry; a Maintenance
badge, a fleet-level count, and an escalating day-counter keep a forgotten device
visible instead of silently blind. See
[ADR-056](../adr/ADR-056-device-maintenance-mode.md).

## Web telemetry surface

The React client renders this telemetry through a thin adapter over uPlot
(canvas-2D): React owns the chrome, the renderer owns the pixels via typed
arrays, so a polling refresh never reconciles thousands of points. The adapter
([`TimeSeriesChart`](../../web/src/features/devices/charts/TimeSeriesChart.tsx)) is
the only module importing uPlot and is code-split into a dedicated `charts`
chunk with its own budget in [`.size-limit.json`](../../web/.size-limit.json).

Charts draw the window that was asked for. The device metrics endpoint returns a
bucket grid derived from the request rather than from the samples the store
holds ([API Reference](../architecture/API-Reference.md)), so a wide window over a sparsely
reporting device arrives as that whole window with `null` in the buckets nobody
reported. [`aligned-data.ts`](../../web/src/features/devices/charts/aligned-data.ts)
projects each column onto that grid — padding or trimming so a reading can never
land against the wrong instant — maps `null` to `NaN`, and keeps `spanGaps` off
on every drawn series including the band edges. The hole stays a hole: a line
across a device-offline stretch would assert measurements nobody took.

The device-detail panel
([`DeviceMetrics`](../../web/src/features/devices/DeviceMetrics.tsx)) shows the
current edge-health anomaly rate, per-family metric timelines (avg line plus a
band whose `min_max_source` provenance is labelled honestly — `avg_of_60s` is
min/max across the 60 s averages, not host extrema) over the window a preset
chooses. Which dimensions broke pattern is ranked on the device itself and
arrives inside the alert, so the panel reads its window and asks the server
nothing else. The virtualized device grid and the dashboard carry only scalar
health badges
([`HealthBadge`](../../web/src/features/devices/HealthBadge.tsx),
[`FleetHealth`](../../web/src/features/devices/FleetHealth.tsx)) — no per-device
series on the grid.
