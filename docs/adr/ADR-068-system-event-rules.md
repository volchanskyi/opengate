---
number: 68
title: Rules for failures that cross no threshold
---

# ADR-068 — Rules for failures that cross no threshold

## Context

A hung task, a process killed for memory, a disk reporting errors: none of these
move a reading past a line. They are events, and a threshold rule cannot see
them.

## Decision

**Four curated Linux rules plus one rolling count**, read from the system
journal: hung tasks, out-of-memory kills, disk errors, and service failures,
with a per-service count beside them.

**A rule matches on exclusions as well as alternatives**, because every
subsystem that logs a failure also logs recoveries that read the same way.

**A cursor, because polls overlap on purpose.** The poll window reaches back
further than the interval, so nothing falls between two polls; the cursor is
what stops the overlap being counted twice.

**A record arriving late is dropped rather than duplicated** — a deliberate
choice, because a duplicate alert costs a technician's attention and a missed
late record costs one line of a log that is still on the machine.

**A saturated poll is counted, the records it lost are not.** A poll that
returned as much as it was allowed to says so, rather than reporting a number
that is really a ceiling.

**A record a curated rule already explained does not also feed the per-service
count**, or one failure would be reported twice at two levels of detail.

**Maintenance suppresses the window rather than deferring it.** Patching is
disruptive by design; the events it causes are not findings, and holding them to
deliver later would deliver a pile of noise the moment the window closed.

**One bounded sink per device, and every limit counts what it cost.** The sink
has a ceiling; a refusal is recorded as a refusal, so the ceiling is visible
rather than silently shaping what anyone sees.

## Consequences

**Ranking what broke runs on the device and travels with the alert.** The agent
compares each dimension's behaviour during the event window against the stretch
before it, blends three signals into one score between zero and one, and sends
the ranking inside the alert. The central correlation endpoint it replaced is
removed outright.

Every run is bounded — how many dimensions are examined, how many windows, and
how long it may take — and a degenerate window answers with a number rather than
a not-a-number. The read is a snapshot of the local store, so a ranking running
while new samples land sees one consistent view.
