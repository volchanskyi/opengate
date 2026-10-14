---
number: 88
title: A benchmark measures the code, not the harness
---

# ADR-088 — A benchmark measures the code, not the harness

## Context

The handshake benchmark's measured region included its own pipe and the
scheduler moving between two goroutines. The number moved when the runner was
busy and did not move when the handshake changed.

The commit-time benchmark run compared nothing, so a certificate template moved
into a helper put 1.5 KB on the heap per signature and was reported by the
nightly eight nights running. Two agent suites, Edge Sentinel and the local
metrics store, were run by no workflow, and the sampler's figure was its sleep.

## Decision

**The measured region is the code under test**, with setup and transport
outside it.

**No benchmark toggles its own clock inside the measured loop.** Stopping and
starting the timer per iteration measures the timer.

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

A new benchmark is gated on allocations and on the nightly's time band only once
a baseline run from the workflow has measured it.
