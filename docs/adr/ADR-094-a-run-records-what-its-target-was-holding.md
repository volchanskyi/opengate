---
number: 94
title: A run records what its target was holding
status: Accepted
date: 2026-09-02
---

# ADR-094 — A run records what its target was holding

## Context

Two nightly load runs on the same commit, with the same inputs and the same
fleet result, produced opposite outcomes. One passed. The other's target
process was replaced 90 seconds after its fleet connected — the container was
killed for memory — and every scenario after that measured a different server
than the scenarios before it. The k6 half recorded a 0.74% error rate and 45
failed checks against a server that was dying, and those rows counted as
produced.

The run failed only because the replacement happened to break the *last*
scenario. Had it happened earlier, or later, the night would have entered the
trend as a measurement.

The harness already had everything it needed to know. It fetches the target's
exposition to read registration timing, and the four families that answer this
are on that same page, published by the client library's Go and process
collectors rather than by anything this codebase counts: `go_goroutines`,
`process_resident_memory_bytes`, `process_open_fds`, `process_start_time_seconds`.
It read none of them.

This is [ADR-090](ADR-090-a-run-is-gated-on-its-own-verdict-not-on-its-failure-count.md)'s
subject — a run is gated on its own verdict — extended to the half of the
verdict the run was not computing. It is also the run-level instrument for
[ADR-096](ADR-096-a-counter-of-a-resource-is-not-a-measurement-of-it.md): the
conservation test holds the property at commit time, and this holds it nightly
against the deployed thing under real load.

## Decision

**A run brackets itself with two readings of its target, and records both.**
The first is taken immediately before the workload starts, the second after the
fleet is wound down. The harness holds its fleet for the whole night — the
generators beside it run inside that hold — so the bracket covers every
scenario. Both readings enter the bundle's `Observations`, not just their
difference: a bundle is read years after the metrics store forgot the night, and
a delta cannot be re-divided by a denominator a later reader wants to change.
`process_open_fds` travels with them because it is what separates a goroutine
leak from a socket leak; its flatness through a 344 MiB climb is what ruled
sockets out.

**A target replaced mid-run makes the run invalid, not failed.** The numbers
either side of the restart were measured against two different processes.
Absorbing that as data costs two nights, for the reason
[`validity.go`](../../server/tests/loadtest/validity.go) already states about
partial nights: the figures pull the window median down, and the next genuinely
slow night compares favourably against the lowered median and passes.

**A target that did not give back what it took makes the run failed.** That is
a finding about the system rather than a reason the run measured nothing, so its
rows still enter the trend — a system that is leaking is exactly what a trend is
for. The figure is expressed **per completed operation**: every machine that
connected and every relay session the harness answered is one operation the
target took something for. Per operation rather than absolute, because an
absolute figure has to guess at a floor that changes whenever anything else in
the process does.

**Goroutines carry the gate; resident memory is recorded beside them and not
gated.** Goroutines are whole numbers, a settled server holds a stable count,
and the defect that motivated this retained exactly two per session — so half of
one per operation sits far below a regression and far above noise. Resident
memory cannot do the same work inside a single run: the Go runtime does not
return freed arena to the operating system promptly, so resident growth cannot
be told apart from a working set that simply got bigger, and a band wide enough
not to fire on that is too wide to catch the leak it would be for. The
instruments for resident memory are the container's own limit, watched
continuously by
[`alert-rules.yml`](../../deploy/grafana/provisioning/alerting/alert-rules.yml)
rather than twice a night, and the per-operation figure this run records, where
a slope across nights can be read off the trend. Stating that boundary is part
of the decision rather than a gap in it.

**The reading is taken once the target has stopped putting things back.**
Teardown is not instantaneous on the far side of a network, and a reading taken
the moment the last machine hangs up counts connections that are still closing.
The end reading polls until the goroutine count stops falling or a stated budget
expires, and records what it has either way — waiting longer for a number to
improve is how a gate stops being one.

**A bundle nobody wrote is silence, not a pass.** The shell completeness gate
folds the harness's verdict about its target into the night's own, because a run
has one verdict however many places compute it — the same reasoning that already
holds the error-rate ceiling equal between
[`loadtest-run-completeness.sh`](../../scripts/loadtest-run-completeness.sh) and
`validity.go`. When no bundle exists the gate says nothing about the target and
still applies its own reasons to fail the night.

**It is read off the exposition, not off the cluster.** No kubeconfig, no
`restartCount`, no pod UID, no Prometheus round trip. That is what lets the same
reading work for the volume and scaling families, whose target runs in a compose
stack on a GitHub runner where there is no cluster to ask — and it makes the
bundle authoritative past the store's thirty-day retention.

## Consequences

The incident that motivated this would have been a red run on the first night,
with the cause named in the bundle rather than reconstructed six hours later
from three metric series and two job logs.

The harness now fetches its target's exposition twice per run instead of once,
and waits up to half a minute at the end for the target to settle. Against a run
that holds a fleet for eight minutes that is not a cost worth naming.

The exposition it reads is the server's cluster-only listener
([ADR-095](ADR-095-the-server-has-two-listeners.md)), so the URL the harness is
given names that port. A harness pointed at the API port would find the
single-page application's fallback where the exposition should be and record a
target it never measured, which is why the port is read back against the chart's
own in [`loadtest-workflow.test.sh`](../../scripts/tests/loadtest-workflow.test.sh).

The hold gained a write for the same reason the bracket exists. The heartbeat
runs agent to server, so a held machine receives nothing from a healthy server
either: reading harder can never tell a quiet server from a dead one, and the
run that found this reported `Agents: 100/100 succeeded, Failures: 0` for an
8m30s hold whose connections were severed at the four-minute mark. A held
machine now sends an `AgentHeartbeat` on an interval, which fails against a peer
that is gone; the bundle counts the machines it lost that way, and records zero
when it lost none.
