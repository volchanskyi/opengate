---
number: 88
title: A benchmark measures the code, not the harness
---

# ADR-088 — A benchmark measures the code, not the harness

## Context

The handshake benchmark's measured region included its own pipe and the
scheduler moving between two goroutines. The number moved when the runner was
busy and did not move when the handshake changed.

Six protocol benchmarks handed their operation to a helper as a function value,
and Go's `b.Loop` keeps alive only what is written inside the loop, so the
encoder and decoder were optimised away and their baselines recorded 1.7 and
4.4 ns. A nightly that found a one-night outlier failed on that one reading.

The commit-time benchmark run compared nothing, so a certificate template moved
into a helper put 1.5 KB on the heap per signature and was reported by the
nightly eight nights running. Two agent suites, Edge Sentinel and the local
metrics store, were run by no workflow, and the sampler's figure was its sleep.

## Decision

**The measured region is the code under test**, with setup and transport
outside it.

**No benchmark toggles its own clock inside the measured loop.** Stopping and
starting the timer per iteration measures the timer.

**The operation is written inside the loop.** A loop whose body only calls a
function value measures a call the compiler may hollow out, and
[`bench_loop_guard_test.go`](../../server/internal/testutil/bench_loop_guard_test.go)
reads every Go test file and refuses one.

**Each lazily loaded bundle carries its own size budget.** One large dependency
inside a shared budget leaves every other lazy chunk unmeasured, because the
shared figure is dominated by the one thing.

**A value fixed for the life of an object is computed once.** The certificate
authority hash was being recomputed per connection inside the loop that was
supposedly measuring the connection.

**A transport is replayed, not run.** The relay's forwarding benchmark hands the
production WebSocket adapter a prepared frame on an in-memory connection and
discards what it writes, the way the handshake benchmark replays its request, so
the figure is the per-message work rather than a socket and two goroutines.

**Work that waits by design is measured in CPU time.** The sampler sleeps between
two CPU readings, so its wall time is mostly the sleep, and a doubling of its real
cost would read as a 7% rise. Its benchmark reads the process CPU clock.

**The baseline holds CI's readings and no other.** Each row is the median of the
nightly runs that measured the code the row describes. A workstation's numbers
come from different hardware and are never written into it. A benchmark CI has
not yet measured has no row: it is reported without a limit until a baseline run
includes it, and a row naming no benchmark is refused.

**A benchmark is re-baselined by date, not by a second commit.** A row may carry
a `history_from` date in place of numbers. The nights before it are ignored; the
row is reported without a limit until three CI nights exist after it, and from
then on its time and its allocations are held to those nights' median. A
benchmark changed on purpose, or a new one, lands with its date set to the day
after the commit, and starts gating on its own with numbers that are still only
CI's.

**A slow Go timing is measured again before it fails a night.** The nightly
re-runs each Go benchmark past its line ten times and compares the runs with the
window's nights through `benchstat`, Go's documented method
([`benchmark-confirm.sh`](../../scripts/benchmark-confirm.sh)); it fails only when
the change is significant and the new median is past the line, and the summary
quotes benchstat's verdict. A Rust timing is judged on its one reading, because
Criterion already samples within the run, and allocation counts stay one-shot,
because they are deterministic. A baseline run that finds a regression still
uploads its candidate.

**Allocations are held at commit time; time is held nightly.** Allocation counts
and bytes depend on the code and the processor count, not the machine's speed. At
the runner's processor count a workstation reads CI's figures within one
percent, against 1.9% at its own count, so the gauntlet runs the Go benchmarks at
that count and holds them to the baseline's tolerance.

**Every benchmark suite runs nightly.** The nightly runs every Criterion suite of
the three crates that carry one, and the gauntlet runs each once, so a suite that
stops compiling or panics fails the commit.

## Consequences

Guards refuse a clock toggled inside a measured loop and a bundle subtracted
from a budget without a budget of its own, so the shapes cannot come back.

A new benchmark is gated on allocations and on the nightly's time band once a
baseline run from the workflow has measured it, or three CI nights after its
`history_from` date.

A one-night outlier no longer fails the nightly, at the cost of ten more runs of
each flagged benchmark.
