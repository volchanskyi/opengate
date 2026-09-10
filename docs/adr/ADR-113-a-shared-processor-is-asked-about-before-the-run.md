---
number: 113
title: A shared processor is asked about before the run, not during it
status: Accepted
date: 2026-09-10
---

# ADR-113 — A shared processor is asked about before the run, not during it

## Context

Staging and production share one worker node, so a profile that runs there
declares what the run must not push that node past. Three ceilings are declared
and all three were read between every phase: processor, memory, and the disk the
database writes into.

The processor one refused the staging nightly.
[Run 34459565632](https://github.com/volchanskyi/opengate/actions/runs/34459565632)
stopped after its ramp phase with *the node's processor is 108% committed against
a limit of 85%*, having connected two hundred and fifty machines. It produced no
measurement, so every limit in the profile then failed for want of a row, and the
night went red three jobs deep.

The reading was correct. It is the node's one-minute average across its two
processors, taken the way
[ADR-108](ADR-108-the-venue-picks-how-a-busy-machine-is-read.md) settled for a
guest, and the same node read between 9% and 39% on two samplings taken when
nothing was running on it. What had changed in the minute before the reading was
the run: a fleet arriving, a browser-side generator beside it, and the staging
server doing the work both were asking for.

So the ceiling stopped the run for doing what it was asked to do, and it will do
so on every night that succeeds in offering load. A ceiling a working run cannot
pass is not a safety ceiling.

The reason it reads that way is what the resource is. Memory and disk get used
up: what the run puts there is gone until it gives it back, and past the ceiling
the node has nowhere to put the next thing — so the run's own share is exactly
what those ceilings are asking about. Processor time is not used up, it is taken
in turns. A node whose processors are over-committed serves everything more
slowly in proportion to what each pod was promised; it does not run out, and
nothing is evicted for it. The comment beside the check said the kubelet starts
choosing which pods to evict, and that is true of the other two ceilings and of
neither this one.

What actually protects production from the run is not a reading at all. Every pod
on that node has a share it is guaranteed and a cap it cannot exceed, and the
kernel keeps both continuously. Production's server holds a quarter of a
processor as both, so it has that quarter whatever the run does; the run's own
pods are capped at four hundred millicores each, which is a figure somebody
chose rather than one the run can exceed by working harder.

## Decision

**The processor ceiling is read once, before the run offers anything. The
ceilings on room the node can run out of are read for the whole walk.**

There is exactly one moment when a reading of a shared node's processors is a
statement about the neighbour rather than about the run, and that is before the
first machine dials. That is the moment the question *is there room beside
production tonight* is asked, and a no there is a night that should not start.

`CheckRoomToStart` reads all three and runs before the first phase.
`CheckRoomToContinue` reads memory and disk and runs everywhere else. Naming them
apart is the point: a call site cannot ask the wrong one by accident, and neither
name can be read as "check safety" and left to mean whatever the reader assumed.

## Consequences

A staging night that offers load is no longer refused for having offered it, and
a night that starts on a node production has already filled still does not start.
The disposable venues are unaffected: they declare no processor ceiling at all,
for the reason they always did.

What this gives up is the ability to stand down mid-run if production becomes
genuinely busy underneath. That was never available: the reading could not tell
production's work from the run's own, so a mid-run refusal was a coin flip
weighted by how well the run was working. What replaces it is the run's own cap,
which is smaller than production's guarantee and enforced by the kernel rather
than by a look.
