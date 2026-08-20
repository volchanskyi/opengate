# Device Health

Every managed machine measures itself once a second, keeps that history locally,
and reports a small fixed set of readings to the server. This chapter explains
what those readings mean, when one is unavailable and why, and what you see on
the device page.

Detection built on top of these readings is in
[Alerts and Rules](./Alerts-and-Rules.md).

## The vitals contract

A device reports a **fixed vocabulary** of readings. It is fixed on both sides:
the agent can only send names from the list, and the server only stores names
from the list. A name outside it is dropped and counted, so nothing an agent
sends can quietly enlarge what the platform stores.

Each reading is a 60-second value. Where a spike inside that minute is itself the
signal, the peak is reported beside the average.

**The vocabulary is capped.** A Linux device occupies at most 24 series centrally:
the eighteen readings below, plus its overall anomaly rate and five per-family
anomaly rates. What the central store has to pay for is how many distinct series
a fleet creates, not how often a machine samples — so sampling every second costs
nothing centrally, while adding a nineteenth reading is a decision about the cap
itself. Eight of the twenty-four come from Linux-only kernel sources; a host that
supplies neither reports those as unsupported.

### Host resources

| Reading | What it means | Peak also reported |
|---|---|---|
| `cpu.total` | Processor busy, as a percentage | Yes — `cpu.total.max` |
| `mem.used_percent` | Memory in use, as a percentage | Yes — `mem.used_percent.max` |
| `net.rx_bps` | Bytes per second received | Yes — `net.rx_bps.max` |
| `net.tx_bps` | Bytes per second sent | Yes — `net.tx_bps.max` |
| `disk.used_percent` | **The fullest mount**, as a percentage | No — it moves too slowly for a peak to mean anything |
| `disk.mounts_critical` | How many mounts sit at or above the critical-usage mark | No — it is already a count of threshold crossings |

Two dimensions answer to a second name: `mem.used` and `disk.used` resolve to
`mem.used_percent` and `disk.used_percent`, so a rule written against either name
watches the same reading.

> **Why the fullest mount, not an average?** A file server with a 40 GB system
> volume at 98% beside a 2 TB data volume at 10% averages to about 15%. A rule
> watching the average never fires for the volume that is about to fill. The
> fullest mount is the one an operator can act on.

### Stall vitals — how long work waited

| Reading | What it means |
|---|---|
| `stall.cpu.some` | Share of the minute some task spent waiting for processor time |
| `stall.mem.some` | Share of the minute some task spent waiting on memory |
| `stall.mem.full` | Share of the minute **all** work was stalled on memory |
| `stall.io.some` | Share of the minute some task spent waiting on disk |
| `stall.io.full` | Share of the minute **all** work was stalled on disk |

Stall vitals answer the question a utilisation percentage cannot: *is this
machine slow for the people using it?* A host can sit at 60% processor and still
be unusable because everything is queued behind disk.

These come from the Linux kernel's own pressure accounting, so they cost the
agent one file read and no extra measurement. An agent running inside a container
reads its own container's pressure, never the host's.

> The processor `full` line is not reported: the kernel defines it as always zero.

### Disk performance — is the disk slow?

`disk.used_percent` answers "is the disk full". These answer "is it slow", which
is a different question about a different piece of hardware — capacity belongs to
a mount, service time belongs to a physical device.

| Reading | What it means |
|---|---|
| `disk.await_ms` | Average time one I/O took to complete |
| `disk.await_ms.max` | The worst such average inside the minute |
| `disk.queue_depth` | How many I/Os were outstanding on average |

**Each of these reports the worst device, chosen per reading independently.** The
device with the slowest service time and the device with the deepest queue are
routinely different — a wearing system disk beside a data disk taking a backup —
so averaging across them would describe neither. Per-device detail travels inside
alert evidence instead.

Measured devices are the whole block devices the kernel lists (partitions are
excluded by construction), minus loop, ram and zram pseudo-devices. Mapper and
RAID devices are included: encryption and RAID overhead is latency the user
waits for.

> **There is deliberately no "disk busy %".** SSDs and NVMe serve many I/Os in
> parallel, so a busy percentage pins at 100% with plenty of headroom left. Queue
> depth keeps scaling where a busy percentage saturates, which is what separates
> "busy but healthy" from "overloaded".

### When a reading is unavailable

A machine that cannot measure something reports it as **unsupported** and sends
nothing for it. It never sends zero.

| Situation | Effect |
|---|---|
| Host with no kernel pressure accounting | The five `stall.*` readings are unsupported |
| Containerized agent | Disk-performance readings are unsupported — the kernel's disk counters cover the whole host, not one container. `stall.io.*` still works and remains a genuine I/O-wait signal |
| Virtual machine | Everything is reported normally. Guest-measured latency already includes host contention and volume throttling, which is exactly what makes the customer's application slow |

**An absent reading is absent, never zero.** Zero is the answer for a machine that
was measured and never stalled — a different fact from a machine nobody can
measure. Rules understand the difference: a rule whose reading is unavailable
reports itself `unsupported` on that machine rather than silently passing.

## Anomaly state

Alongside the vitals, each device judges whether its own readings are behaving.
The agent trains a small model locally over the metric families it samples and
votes each new reading against it. Nothing is sent anywhere to make that
judgement — the analysis happens on the machine.

What reaches the server is the summary:

- the device's overall **anomaly rate**;
- the same rate **per metric family**;
- a compact record of which recent readings were flagged, which is what lets a
  chart show *when* a machine started misbehaving rather than only that it is.

The dashboard turns the overall rate into the **Healthy / Watch / Anomalous**
bands described in [Fleet and Devices](./Fleet-and-Devices.md#dashboard).

> The summary carries the version of the sampler and model that produced it. Two
> rates from different model generations are two different measurements, and
> charts keep them apart rather than drawing them as one line.

## The telemetry pane

The device page draws the window you ask for:

- **Anomaly rate** for the device, current value and over time.
- **Per-family charts** — an average line plus a band showing the spread inside
  each bucket. The band is labelled with what it actually is, so a band derived
  from 60-second averages is never presented as the machine's true peak and
  trough.
- **Presets** for the window; a wider window means coarser buckets.

**Gaps stay gaps.** If a device reported nothing for a stretch — it was offline,
or in maintenance — the chart leaves that stretch empty instead of drawing a line
across it. A line across a gap would assert measurements nobody took.

The device grid and the dashboard carry only health badges, never per-device
charts, so a large fleet list stays fast.

## Maintenance mode

Put a device into maintenance before disruptive host work — package upgrades,
service restarts, reboots — so the disruption you intended does not look like a
fault.

### What maintenance does

| While in maintenance | Effect |
|---|---|
| Sampling | Stops, so the spike from a reboot is never recorded |
| Discovery | Stops, so a service churn is not read as a changed footprint |
| Log collection | Stops |
| Alert evaluation | Suppressed, including the system-event rules a reboot would otherwise trip |
| Remote management | **Stays live** — the control channel stays connected, so the machine is still distinguishable from one that crashed, and you can still take it over |
| Open incidents about this machine | Stay open — the silence is the silence you asked for, not a recovery |

### Using it

1. Open the device page and select **Enter maintenance**, with a reason. Any
   member of the tenant that owns the device can do this.
2. Do the host work.
3. Select **Exit maintenance**.

On leaving maintenance the agent re-baselines its anomaly detection, so the state
the machine is in after the change becomes the new normal.

> **Maintenance never expires on its own.** It is set and cleared by a person. A
> maintenance badge on the device, a count on the dashboard, and a day counter
> that escalates as the days pass keep a forgotten device visible instead of
> silently unmonitored.

## Offline machines lose nothing

A device stores its own metric history locally, so a network outage or a server
restart does not create a hole in what you can see afterwards.

When the agent reconnects:

1. It asks the server for permission to catch up. The server grants a rate, or
   defers it while live traffic is heavy — a fleet reconnecting at once cannot
   overwhelm the ingest path.
2. It sends the most recent window first, on the same one-minute grid the live
   stream uses, then progressively older and coarser history.
3. Each batch is acknowledged before the next is sent, so an interrupted catch-up
   resumes where it stopped rather than starting over.

Every historical point is written at the time it was actually measured, not at
the time it arrived, so backfilled history lands in the right place on a chart.

For history older than the server keeps, or for full one-second detail, an
administrator can pull a bounded window directly from a single machine.

## Related

- [Alerts and Rules](./Alerts-and-Rules.md) — turning readings into alerts
- [Investigations](./Investigations.md) — working what the alerts produced
- [Endpoint Logs](./Endpoint-Logs.md) — reading the machine's own log
