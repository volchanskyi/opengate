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

A night compared with nights on other machines measured the machine. The hosted
runners come from at least six processor models, and one model is not steady:
the handshake read 3 512, 3 727 and 10 311 ns on three nights of one model. One
commit's `decode_server_hello` read 8.36 to 17.15 ns on different nights, and
the nightly stayed red six nights on unchanged code; on one machine the two
commits it compared read 7.94 and 7.99 ns.

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
not yet measured carries its name alone, held to no allocation count until a
baseline run includes it, and a row naming no benchmark is refused.

**An allocation count is re-baselined by date, not by a second commit.** A row
may carry a `history_from` date in place of numbers. The nights before it are
ignored; the row is reported without a limit until three CI nights exist after
it, and from then on its allocations are held to those nights' median. A
benchmark changed on purpose, or a new one, lands with its date set to the day
after the commit. Allocation counts are one-shot, because they are deterministic,
and a baseline run that finds a regression still uploads its candidate.

**Time is judged against an anchor commit on the same machine.** Each night
builds the commit `benchmarks/baseline.json` names as its `anchor` beside
tonight's, runs each language's whole suite at the two in turns, ten turns a
side, and compares the turns through `benchstat`, Go's documented method
([`benchmark-anchor.sh`](../../scripts/benchmark-anchor.sh)). A Rust turn reads
each benchmark's Criterion mean, compared the same way. A slowdown fails the
night when benchstat finds it significant and it is past 10%; the machine cancels
out, since both sides ran on it. The anchor moves only by a commit accepting a
change, the same commit on both sides is recorded and not judged, and a benchmark
absent at the anchor is reported until the anchor moves. This is the method Go's
own performance dashboard uses, and the relative one continuous-benchmarking
tools prescribe for shared runners. Comparing only nights on the same processor
model was weighed and rejected: over 29 Go nights it left 248 of 497 readings
unjudged and still raised one alarm on unchanged code. The Rust turns take about
50 minutes a night.

**The baseline names every benchmark the suites run.** A benchmark it names that
tonight did not run fails the night, so a deleted or renamed benchmark never
leaves judgement silently; removing or renaming one edits the baseline in the
same commit. The gauntlet compares the names its Rust test-mode run prints with
the baseline's Rust rows both ways
([`benchmark-local-check.sh`](../../scripts/benchmark-local-check.sh)), and refuses
a Go row naming no benchmark.

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

A new benchmark is gated on allocations once a baseline run from the workflow
has measured it, or three CI nights after its `history_from` date, and on time
once the anchor moves past the commit that added it.

A slow machine no longer fails the nightly, at the cost of every suite running
twenty times a night.
