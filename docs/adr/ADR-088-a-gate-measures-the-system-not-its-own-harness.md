---
number: 88
title: A gate measures the system, not its own harness
status: Accepted
date: 2026-08-30
---

# ADR-088 — A gate measures the system, not its own harness

## Context

Two gates were reporting numbers that were mostly about themselves.

**The benchmark trend gate** went red on 2026-08-30:
`BenchmarkHandshaker_PerformHandshake` at 27196 ns/op against a window median of
17241, the sixth of six nightly readings that had roughly doubled over a
fortnight — 13323, 16941, 20858, 20645, 24439, 27196. Read as a statement about
the code, that is the per-connection cost of the agent handshake doubling with
nobody able to say what moved.

Nothing had moved. Five consecutive runs of the committed benchmark, one machine
and one commit, measured 22643, 26728, 28909, 36275 and 58060 ns/op — a 2.6x
spread against a gate whose band is 50% and whose sample size is one.

The harness was the cause, and specifically the repair the previous red gate had
received. A deadline had been added inside the measured loop, billing the test's
own plumbing to the server's handshake; the fix wrapped that plumbing in
`b.StopTimer()` / `b.StartTimer()`. Both call `runtime.ReadMemStats`, which stops
the world, so every iteration paid two stop-the-world pauses and restarted the
scheduler cold into the region being measured. Three shapes, same commit, same
machine, six samples each:

| Shape | median ns/op | run-to-run spread |
|---|---|---|
| Committed: pipe, goroutine, per-iteration clock toggle | ~110000 | 4.0x |
| Toggle removed | ~15950 | 1.09x |
| Scripted request, no goroutine and no pipe | ~5620 | 1.18x |

The handshake costs about 5.6 µs. The gate had been watching a figure roughly
80% of which was `net.Pipe` scheduling. A CPU profile of the remaining work says
where it actually goes: `crypto/x509.parseCertificate` at 47.6% cumulative, the
SHA-512 blocks at 16.4%, allocation and collection at about 19%.

**The whole-app JavaScript budget** sat 2.43 kB under its 250 kB cap. The debt
register's prescribed remedy was to find what two routes each carried a private
copy of and give it a shared chunk. Dumping rollup's module-to-chunk map says
there is nothing to find: **zero** modules are rendered into more than one chunk.
What the budget actually holds is one lazy dependency — the terminal engine, at
83 kB gzipped, a third of the whole number — measured together with every route
the application ships. The charting engine was already split out and budgeted on
its own for exactly that reason; the terminal engine was not.

## Decision

**A benchmark's measured region is the code under test.** The handshake
benchmark replays a fixed request from a `bytes.Reader` and discards the reply:
the agent's side of a strict request/response exchange is a byte slice known
before the run starts, so it needs no peer, no goroutine, and no deadline to
bound one. `BenchmarkHandshaker_PerformHandshake_FastPath` is added alongside it,
because a fleet-wide reconnect storm arrives on the 0x14 path and that cost was
measured by nothing.

**No benchmark toggles its own clock inside the measured loop.**
[`benchmark-harness.test.sh`](../../scripts/tests/benchmark-harness.test.sh)
refuses `b.StopTimer()` / `b.StartTimer()` below the function body's own brace
depth, and holds the committed baseline and the benchmark set in agreement in
both directions, so a renamed benchmark cannot leave a baseline row gating
nothing.

**Each lazy engine carries its own budget.** `@xterm` gets a named `terminal`
chunk beside `charts`, subtracted from the application total and given a limit of
its own. Splitting is not an exemption:
[`bundle-budget-coverage.test.sh`](../../scripts/tests/bundle-budget-coverage.test.sh)
fails any chunk that is subtracted from the total without gaining a budget, and
any hole in the glob naming a chunk vite does not create. The application total
is re-cut to its measurement rather than left at a number nothing can reach —
subtracting 83 kB and keeping the old cap would retire the budget as surely as
raising it.

**The CA certificate hash is taken once.** A manager's CA is fixed for its
lifetime and both handshake paths need the same digest, so it is computed in
`NewHandshaker` instead of over the whole CA DER on every connection. On the fast
path that hash was half the work that was not certificate parsing.

## Consequences

The handshake baseline is rebased, and the allocation figures move with the shape
— 3736 B/op and 49 allocs/op cold, 3576 and 47 on the fast path — which the gate
compares at ±2% and which are machine-independent. The ns/op anchor is not: the
same three benchmarks measured on the runner and on a workstation pinned to four
processors disagree by between 0.92x and 1.86x. It is set from the conservative
end of that range, and it backstops only the cold-start case; the 14-day window
median is the rule that actually holds the number.

The numbers a reader of either gate sees are now about the thing the gate names.
The application's JavaScript total drops from 247.57 kB to 164.94 kB against a
180 kB cap, and the 83 kB that moved is gated on the terminal line rather than
uncounted — a terminal-engine upgrade and a route that grew are now two different
failures instead of one.

Neither guard can prove a harness is honest in general. Each refuses one shape
that has already produced a false reading, which is the shape of every gate in
this repository: not a proof, a specific defect that cannot recur silently.

## Alternatives considered

**Rebase the benchmark baseline and leave the harness.** The register's own
fallback. It clears the red and keeps a measurement whose run-to-run spread is
four times the band it is judged against, so the next false regression is a
matter of when.

**Raise the whole-app budget to 260 kB.** The cheapest way to green, and the one
the register names as how a budget stops being one. It also leaves the terminal
engine and the routes sharing one number, so neither is attributable.

**Leave the CA hash where it was.** Correct but wasteful, and the entry that
prompted this work asks for the per-connection cost of the handshake — hashing an
immutable certificate on every connection is part of that cost and none of it is
needed.
