---
number: 90
title: A run is gated on its own verdict, not on its failure count
status: Accepted
date: 2026-08-31
---

# ADR-090 — A run is gated on its own verdict, not on its failure count

## Context

The QUIC load harness already knew a run has three outcomes, not two. Valid and
failed are both measurements — one of a system that held, one of a system that
did not — and invalid is the third: the run did not measure the system at all.
[`Classify`](../../server/tests/loadtest/validity.go) works that out from the
scenarios that produced rows, the generator's own headroom and each phase's
error rate, and writes the answer into the evidence bundle.

Nothing read it. The harness's exit code came from the failure count alone, so a
run whose agents produced no result each — rather than a failed one each —
counted zero failures and was indistinguishable from a clean run. The
performance stack's volume family passed on `Agents: 0/500 succeeded`,
`Failures: 0`, `bundle.json (invalid)`, step green.

Two separate defects made that shape reachable, and it is worth naming both
because they fail in opposite directions:

- The profile-driven path read the fleet's results **before** winding it down. A
  machine reports once, when its own life ends, so a fleet still holding its
  level had reported nothing. A run that held five hundred machines for six
  minutes produced the same account as one that connected nobody.
- The exit code collapsed two of the three outcomes, so that account passed.

The same gap existed on the other side of the fence. The staging load test wrote
its completeness verdict into an artifact the publish job downloaded and never
opened, so ten consecutive nights of `rps 0, error_rate 1` entered the trend and
took the fourteen-day window median from 113 to 57 — which raises the bar a
genuine collapse has to clear before the gate fires. One partial night quietly
costs two, which is the entire reason the invalid classification exists.

## Decision

**A run's verdict is what gates it.** The harness classifies itself on every run,
whether or not an evidence bundle was asked for, and returns one of three codes:
zero for a clean measurement, one for a measurement whose fleet partly failed or
whose gate was breached, and two for a run that measured nothing. Two is a
distinct code because the runner around the harness discards output on it, and
because a message calling it an abort sends a reader looking for a crash that
never happened.

**A fleet is wound down before it is read.** The order is the property, not an
implementation detail, so
[`runProfile`](../../server/tests/loadtest/workload.go) takes the fleet as an
interface that can be stopped and asked, and a test drives a real fleet through
a whole profile to assert that a machine still connected at the end is a machine
the run measured.

**Every workflow that runs a harness reads back what the harness wrote.** This is
[`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md)'s read-back
rule applied to a verdict rather than to a cache key:
[`perf-bundle-verdict.sh`](../../scripts/perf-bundle-verdict.sh) fails a shard
whose bundle says invalid, and the load test's publish job refuses to push rows
for a run its own completeness record classified as invalid. Both fail on an
absent verdict exactly as loudly as on a bad one — a guard that answers yes when
it cannot ask is the false green it was written to close.

**A row is not a measurement.** A scenario whose error rate is past the ceiling
produced exactly as many rows as one that worked, and those rows are zeroes.
[`loadtest-run-completeness.sh`](../../scripts/loadtest-run-completeness.sh) now
holds a scenario to the same ceiling the harness classifies against, so the two
computations of one run's verdict cannot disagree. A scenario that mostly worked
stays: a degrading night is what the trend is for.

## Consequences

A sweep can no longer read as partly working when none of it worked, and a night
that measured nothing cannot lower the bar for the night after it. The cost is
that a genuinely empty run now reds a workflow twice — once where the absence is
known, in the harness, and once at the read-back — which is the trade the
read-back rule already accepts elsewhere.

The ten invalid nights already in VictoriaMetrics are left to age out of the
fourteen-day window. Deleting them means dropping the whole series, good nights
included, because the store's delete API carries no time range; and a depressed
median makes the gate less sensitive rather than wrong, so it blocks nothing
while it drains.
