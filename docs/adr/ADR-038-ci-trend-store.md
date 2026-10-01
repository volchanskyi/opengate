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

**A sample names its measurement and nothing else.** The transport
([`vm-push.sh`](../../scripts/lib/vm-push.sh)) refuses `commit`, `run_id` and
`grade` on a sample, and writes one `ci_run_info{workflow,commit,run_id}` per
push so a night still names the code it ran. A label that changes every run
makes every run a series of its own, which Grafana draws in a colour of its own
and joins to nothing; and the store carries a reading forward only on a series
that holds two, so folding the label away in a query draws a staircase with
gaps. Each
sample carries the time its run started, taken by the workflow's first job, so a
night lands on one date however long it runs and a re-run writes the same point.

**Tonight is judged against the nights before it, not the commits before it.**
The nightly reader ([`vm-query.sh`](../../scripts/lib/vm-query.sh)) takes the
latest reading of each date before tonight's, and the gates compare tonight with
the median over the most recent dates, needing a minimum count of them. A re-run
is kept out by its date. Nights on the same code count: one point per commit
would weigh a bad night on a new commit as much as three good nights on an old
one, and leaving out the current commit's nights would judge a week without a
merge against older code only. Error rate is compared with the same window, and the
count of consecutive bad nights is read from the newest night before tonight.
The dates come from the store's whole retention, so a weekly run has a window.

**Trend panels draw the raw readings**: an instant query of a bare selector over
the dashboard's range, one line per measurement and one point per night at its
own time
([`grafana-trend-panels.test.sh`](../../scripts/tests/grafana-trend-panels.test.sh)).

**The load-test gate reads its baseline back and fails red.** The workflow
splits: the run produces the summary with no environment attached, so scheduled
runs stay unattended; the publish job reads the summary, compares each series
against its window median with a calibrated tolerance plus absolute ceilings,
pushes the current rows, alerts, and only then fails. Pushing before failing
means a regression is still recorded.

**The performance stack and the endurance run join the store and the gate** the
same way, each with a publish job and a gate job of its own
([`perf-vm-push.sh`](../../scripts/perf-vm-push.sh),
[`perf-regression-check.sh`](../../scripts/perf-regression-check.sh)). Their
tolerances were calibrated from the stored bundles, and the calibration split
the legs in two. A leg that holds its load moves little from night to night, so
its window fails the run. A leg driven to or past what its runner can take
swings by orders of magnitude on unchanged code, so its window is reported and
its profile's fixed limits decide.

## Consequences

Retention is the store's, which is 30 days, so a run's own bundle is
authoritative for anything older and the dashboard is a view of it.
