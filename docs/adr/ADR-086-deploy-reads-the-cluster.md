---
number: 86
title: The deploy reads the cluster, and writes no cache
---

# ADR-086 — The deploy reads the cluster, and writes no cache

## Context

The deploy skipped staging when the image digest matched what a build cache said
was deployed. The cache was the record, and the cluster was the thing.

Separately, the deploy's token carries read scope only. Two cache saves were
refused on every run for two months, each printing a warning and exiting zero.
A skip that had been firing on 45.8% of runs went to zero and nothing in any
run's conclusion said so.

## Decision

**The pre-flight reads the running deployment off the cluster.** What is
deployed is a fact the cluster holds; asking it is one call and it cannot drift.

**The deploy declares no cache at all** — not an explicit cache step, not a
toolchain cache, not the cache half of a setup action, whose save is refused
just as quietly. Losing the restore is the price. A permanently refused save
that reports success is not a trade worth making.

**A cache write under a key we chose is read back through the cache API before
the job that wrote it may pass.**
[`assert-cache-written.sh`](../../scripts/assert-cache-written.sh) fails on an
absent key and on a listing it could not read, because a guard that answers yes
when it cannot ask is the false success it exists to close. Where the save
happens in an action's post step, the read-back is a separate job, since nothing
inside a job can see its own post step.

**The exemption is re-earned.** The list of workflows that may not cache is a
statement about tokens. A workflow that gains write scope loses its row and gets
its cache back, in the commit that proves it.

**Production's approval gate is unchanged.** It is gated twice and the skip
never applied to it.

## Consequences

The binary the deploy used to build cold now arrives as an artifact from the
image build, whose token does write.

A cache an action computes and writes on its own — container layers, scanner
databases — names no key a caller can assert, and is outside what this can hold.
Saying so is part of the rule rather than a gap in it.
