---
number: 97
title: The mutation night's time budget
---

# ADR-097 — The mutation night's time budget

## Context

The pre-flight projected each shard as mutants times cost per mutant and cleared
them all. One shard was projected at 31 minutes and was killed at the 90-minute
cap, taking the night's score with it.

The missing term: every mutant gets a time limit of the coverage run's elapsed
time times a coefficient, and a mutant that removes a loop's exit condition never
finishes and holds a worker for all of it. That limit was 46 to 75 minutes on a
shard projected at 31.

It hid well. Such a mutant is recorded as timed out, which is neither a kill nor
a survivor, so it moves no score and appears in no report field. The only trace
is wall clock. It also corrupted the first term, because cost per mutant was
measured as total elapsed time divided by all mutants — dividing one mutant's
one mutant's time limit across every one of them.

## Decision

**A projection states every term of the cost.** The time limit is declared per shard
in [`mutation-shards.sh`](../../scripts/lib/mutation-shards.sh) and added to the
projection.

**Cost per mutant is measured over the mutants that finish.**

**The coefficient is a bound, not a tuning knob.** It is held by
[`mutation-workflow.test.sh`](../../scripts/tests/mutation-workflow.test.sh) to
a value whose limit still fits in what a fully spent shard has left of the cap,
so a mutant that starts blocking between one night and the next — before any run
has declared it — cannot carry the job past the cap on its own.

**Per-shard coefficient overrides are gone.** Two existed and both were
compensating for the mismeasurement above.

## Consequences

The generalisation: wherever a gate answers whether something will fit, the
answer is worth no more than the slowest thing it forgot to add up. A cost no
counter reports is the one to go looking for.
