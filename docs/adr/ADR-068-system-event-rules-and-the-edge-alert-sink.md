---
adr: 068
title: "System-Event Rules Over a Polled Log, and the Bounded Edge Alert Sink"
status: Accepted
date: 2026-08-12
---

# ADR-068: System-Event Rules Over a Polled Log, and the Bounded Edge Alert Sink

## Status

Accepted.

## Context

Every signal the edge raised until now was a number crossing a line. A whole
class of failure never presents that way: a task blocked for two minutes, memory
reclaimed by killing a process, a disk that stopped answering its bus, a
processor slowing itself down under its own heat. The machine reports all of them
about itself, in words, in its own log — and none of them moves a gauge far
enough, or long enough, for a threshold to catch.

The host log reader this has to build on
([`host_logs.rs`](../../agent/crates/mesh-agent/src/host_logs.rs)) is a **bounded
on-demand read, not a stream**: it shells out per call for a window of records
and returns at most a fixed number of them. That single fact decides most of what
follows.

## Decision

**Four curated Linux rules, plus one rolling count.** `hung_task`, OOM kill, ATA
reset and thermal throttle are read from the journal; beside them, one service
producing errors repeatedly inside a day — a thing no individual record says.
A rule is a row of (matcher, meaning), so another platform's reader adds rows
rather than changing a grammar.

**Rules match on exclusions as well as alternatives.** Every subsystem that
reports a failure also reports its recovery, in nearly the same words about the
same component: a disk that resets its link announces the link coming back up, a
throttled core announces its temperature returning to normal. Substring matching
alone looks perfectly green until it pages someone for a machine that just got
better, so every rule ships with a near-miss fixture and the negative half is
tested as seriously as the positive one.

**A cursor, because polls overlap by design.** The poll window reaches back
further than the interval between polls, so a delayed poll cannot leave a gap no
later poll covers — which means the same record arrives repeatedly. A record
newer than the cursor fires; a record *at* the cursor's instant fires only if it
was not already answered for there, which keeps several records sharing one
microsecond from swallowing each other; a record older than the cursor never
fires.

**A record arriving late is lost rather than duplicated.** That is the deliberate
half of the rule above. An alert delivered twice costs an operator more trust
than one delivered never, and trust is the whole asset an alerting system has.

**A saturated poll is counted; the records it lost are not.** A poll returning
the reader's cap saw only the newest end of its window, and how many records fell
off the old end is unknowable. So the poll is counted as an event in itself and
no number is invented for it — the same rule the vitals contract applies to a
reading it cannot take ([ADR-067](ADR-067-disk-performance-vitals.md)). Records
that cannot be placed in time, and services the tracking cap turned away, are
counted separately: three different losses under one counter would be a number
that means nothing.

**A record a curated rule explained does not also feed the per-service count.**
The pack has already said what it was; counting it again toward "this service
keeps failing" reports one event twice, and the second name is the vaguer of the
two. Records the reader could not attribute to a service feed nothing at all,
rather than piling into one unnamed bucket that then fires as though a single
service were broken.

**Maintenance suppresses the window rather than deferring it.** The disruptive
work an admin performs under maintenance produces exactly the records this pack
matches — a host being rebooted stops answering its disks and kills processes —
so holding those records until maintenance ends would page someone for the
maintenance itself ([ADR-056](ADR-056-device-maintenance-mode.md)).

**The read is bounded by what the rules could act on, asked of the pack.** The
level floor is derived from the rules rather than hardcoded at the call site: a
floor written into the reader keeps working right up until someone adds a rule
that watches warnings, which then matches nothing and says nothing about matching
nothing.

**One bounded sink per device, and every limit counts what it costs.** Alerts
from every edge producer land in a single queue. It drops its **oldest** entry
when full, because the newest alert describes what the device is doing now and
keeping a three-day-old one instead answers the wrong question on reconnect. It
admits at most **20 alerts per rolling hour** (§6.6's per-device ceiling), so one
host in a loop cannot drown detection across the fleet; the window rolls rather
than buckets, so a device that spends its allowance in one minute is not deaf for
the other fifty-nine. A suppressed alert is discarded rather than held, since the
ceiling governs what the device *raises* and deferring a storm delivers it late
instead of not at all. Both limits are counted and reported: a suppression nobody
counts is indistinguishable from a quiet device.

**Redaction happens before an alert exists**, not before it is sent
([ADR-049](ADR-049-edge-sentinel-raw-log-privacy.md)). An alert is the one path
that lifts a raw log line off a host outside the Logs pane, so the sink is
allowed to assume everything reaching it is already safe to leave the device.

## Consequences

The fleet gains the failures that never crossed a threshold, at one bounded read
a minute and no central cardinality. The pack is a list of rows, so the rule
catalogue and its bindings can deliver instances without touching this logic, and
another platform's reader adds rows rather than a grammar.

The counts this produces — saturated polls, unplaceable records, untracked
services, alerts dropped and alerts suppressed — currently ride a log line with
their running totals. Carrying them on the wire belongs to the alert transport,
which is where the alert itself starts leaving the device.

Two limits now exist that lose alerts on purpose. Neither is silent, but both
mean a device's alert history is a summary rather than a ledger, and any later
reasoning over alert counts has to read the loss counters beside them.
