---
number: 97
title: A mutant's leash is a term of the budget
status: Accepted
date: 2026-09-03
---

# ADR-097 — A mutant's leash is a term of the budget

## Context

The nightly mutation run of 2026-09-03 lost its canonical score row. One Go
shard, `go-domain-alerts`, was shot at the 90-minute job cap without writing a
report; the artifact set came back short, and the publish job and the score gate
both failed behind it, exactly as they are built to.

The pre-flight that exists to prevent this had cleared the shard four hours
earlier, projecting it at 31 minutes against a 70-minute budget. It was not
wrong about anything it measured. It projected `mutants × per-mutant cost`, and
that is one of two terms.

The other is the leash. gremlins gives every mutant a deadline of the coverage
run's own elapsed time multiplied by `timeout-coefficient`, and the coefficient
was 15 against coverage runs of 185s to 298s — a leash of 46 to 75 minutes per
mutant, on shards whose whole budget was 70. A mutant that removes a loop's exit
condition never terminates and holds a worker for all of it.

Five such mutants are in the tree, each a `CONDITIONALS_NEGATION` on a loop
guard: the VAPID key-padding loop, the MPS accept loop, the alert retention
drain, and two listener guards in the agent API. The retention drain is the one
that ran the job past the cap; it had already spent nearly 13 minutes of a
57-minute leash when the runner was killed, on a shard that had otherwise finished 141 of
its 142 mutants.

The term hid unusually well:

- gremlins records such a mutant as `TIMED OUT`, which is neither a kill nor a
  survivor. It moves no score, and the JSON report has no field for it. The only
  trace it leaves anywhere is wall clock.
- It corrupted the first term as well. The per-mutant costs were measured as
  `elapsed_time / mutants_total`, which divides one mutant's leash across all of
  them. That read `go-updates-certificates` at 21 seconds a mutant when 143 of
  its 144 finished in 106 seconds and the remaining one spent 68.8 minutes, and
  `go-amt` at 13 when its true figure is under 2. It is why the declared costs
  had drifted in both directions at once — the same symptom
  [ADR-083](ADR-083-a-nightly-that-repairs-itself-rather-than-reporting.md)
  answered by re-basing them from those same reports.
- Nothing else was looking. Every downstream gate behaved correctly and reported
  only that the artifact set was short.

## Decision

**The leash is a declared term of the projection.**
`mutation_go_shard_blocking_mutants` states, per shard, how many of its mutants
never terminate, and the pre-flight adds a full leash for each on top of
`mutants × per-mutant cost`. A shard whose projection is mostly leash is over
because its mutants block, not because it grew, and the refusal says so.

**The per-mutant cost is measured over the mutants that finish.** The window is
the end of the coverage run to the last result that is not `TIMED OUT`; blocked
mutants are counted separately rather than averaged into their siblings. Every
Go rate is re-derived this way.

**The timeout coefficient is a bound, not a tuning knob.** It is 3 — gremlins'
own default — giving a leash of 9 to 15 minutes. The 90-minute cap is spent in
four parts, and the budget is what the other three leave: 30s of setup, up to
300s of coverage, the budget, and a headroom equal to one full leash. So a mutant
that starts blocking between one nightly and the next, before any run has
declared it, still cannot carry the job past the cap on its own. The budget is
69 minutes.

The margin is stated in both directions. A leash of 9 minutes is two to three
times the ~290s of the slowest mutant that does finish, which is what keeps a
slow Postgres-backed mutant from being cut off — a false timeout is dropped from
both halves of the score and quietly depresses it. The floor under that margin is
`GOFLAGS=-count=1`, without which a restored test cache answers the coverage run
in half a second and every derived deadline collapses with it.

**`go-domain-alerts` is split** along the seam its own package doc draws: the
room reports fold into (`go-domain-alerts-room`, 62 mutants, 41 minutes) and the
record they fold from (`go-domain-alerts-record`, 80 mutants, 49 minutes). Its
real work was 73 minutes against a 70-minute budget even with the leash removed,
so the split is owed independently of the blocking mutant.

**The per-shard coefficient override is retired.** Two shards carried one because
the baseline granted their blocked mutants an hour. The baseline is now held to a
bound instead, which is strictly tighter than both overrides were. The seam
survives for a shard that one day needs a tighter leash than the rest; what it
may never carry is a looser one.

## Consequences

A blocked mutant now costs a bounded, budgeted 15 minutes instead of an
unbounded 46 to 75. Both shards that were at or past their budget on the failing
night project comfortably inside it: the widest Go shard is 54 minutes against 69.

The declared blocking counts are a measurement like the costs, and go stale the
same way — a new non-terminating mutant is undeclared until a night finds it. The
headroom is sized to absorb exactly one such surprise, which is the bound the
tests hold rather than a hope.

Splitting `internal/alerts` moves it from one directory unit to thirteen file
units, so a new source added there is owned by no shard until it is assigned one —
the partition test fails until it is. That is the same trade `internal/api`
already makes, and it is the point: a new file is a sizing decision, and the
alternative is a shard that grows without anyone choosing it.

`TIMED OUT` remains outside the score in both directions. That is gremlins'
accounting and this decision does not change it; what changes is that the wall
clock it spends is now named somewhere.
