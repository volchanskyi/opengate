---
number: 38
title: CI trends live in VictoriaMetrics
---

# ADR-038 — CI trends live in VictoriaMetrics

## Context

Numeric history about the build — benchmark timings, mutation scores, quality
grades, load-test latency — was kept in files on a branch. Every run rewrote
them, so the history was a merge conflict waiting to happen and nothing could
query it.

## Decision

**VictoriaMetrics holds numeric build trends**, written through one shared
transport that enforces the label convention before anything is sent. Loki holds
logs. Nothing is kept on a branch.

**A sample declares the workload that produced it**, and the gate keys its
baseline by that declaration.
[`loadtest-summarize.sh`](../../scripts/loadtest-summarize.sh) maps a scenario
to a workload name, and a scenario the table does not name cannot enter the
trend — the extraction fails rather than pushing a sample nothing can identify.
Changing what a scenario measures means changing the name, which makes it a new
series that compares against itself rather than against the work it replaced.
The gate groups rather than filters, so this is one query.

**The load-test gate reads its baseline back and fails red.** The workflow
splits: the run produces the summary with no environment attached, so scheduled
runs stay unattended; the publish job reads the summary, compares each series
against its window median with a calibrated tolerance plus absolute ceilings,
pushes the current rows, alerts, and only then fails. Pushing before failing
means a regression is still recorded.

## Consequences

Retention is the store's, which is 30 days, so a run's own bundle is
authoritative for anything older and the dashboard is a view of it.
