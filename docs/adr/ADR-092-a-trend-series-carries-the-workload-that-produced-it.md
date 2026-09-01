---
adr: 092
title: A Trend Series Carries the Workload That Produced It
status: Accepted
date: 2026-09-01
---

# ADR-092: A Trend Series Carries the Workload That Produced It

## Status

Accepted. Extends the gate defined in
[ADR-045](ADR-045-load-test-regression-gate.md); the window rule, the frozen
tolerances and the absolute backstops are unchanged.

## Context

The load-test gate compares each night's figure against the median of the same
series over the previous fourteen days. That is sound while the scenario keeps
measuring the same thing, and the series is identified by
`{source, scenario, phase}` — none of which changes when a scenario is rewritten.

The relay scenario had been filling its latency metric from an unauthenticated
health check. It was rewritten to open the operator's side of a real session and
time its own frame coming back; it kept its name. The first night of the new work
was reported as a regression against the old work's figures: a window median of
1.0 ms judging an 8.1 ms session create, and a throughput floor built from a
request that did no relaying at all. The gate was doing exactly what it was
written to do, against numbers that described different work.

The sample-count guard does not help. It counts nights carrying the series
*name*, so three nights of the replaced workload outvote one night of the
current one and the window rule engages on a baseline that no longer applies.

This is not rare. Four of the six changes to
[`load/k6/scenarios/`](../../load/k6/scenarios) in the preceding six months
altered what a scenario measured — the relay rewrite, narrowing the device read
to a site, driving the API as a member rather than an administrator, and reshaping
the ramp to sit inside the per-IP rate limit. Each one silently poisoned a
fourteen-day window.

The absolute ceilings and floors did not have this problem, because they are
committed numbers recalibrated in the same commit that does the rewriting. Half
the gate was versioned with the workload and half was not.

## Decision

Every canonical row declares the workload that produced it, and the gate keys its
window baseline by that declaration.

- [`loadtest-summarize.sh`](../../scripts/loadtest-summarize.sh) holds
  `workload_name`, which maps a scenario to a name for the work it does. A
  scenario the table does not name cannot enter the trend — the extraction fails
  rather than pushing a sample nothing can identify.
- [`loadtest-vm-push.sh`](../../scripts/loadtest-vm-push.sh) carries the name as
  a `workload` label beside `commit`, `env`, `source`, `scenario` and `phase`.
- [`loadtest-regression-check.sh`](../../scripts/loadtest-regression-check.sh)
  groups its window queries `by (source, scenario, phase, workload)` and looks up
  each row under its own name.

Changing what a scenario measures means changing the name. The rewritten
scenario is then a new series: it compares against itself, or — until the window
holds enough nights of it — against the absolute backstops alone, which is the
cold start the sample-count guard already exists for.

Grouping rather than filtering is what makes this one query instead of many. A
sample stored before the label existed carries no `workload` and groups under the
empty one; no current row keys there, which is the intended reading — what
produced it cannot be established, so it compares to nothing.

## Consequences

Every series pays a one-time cold start. Samples already in the store carry no
workload, so on the first night after this lands each series falls to its
absolute backstops, and the window rule re-engages once three nights of the named
workload exist. That is a real reduction in sensitivity for three nights: the
backstops sit between 18x and 486x above the figures they guard, sized to red on
a collapse rather than on a busy neighbour. `error_rate` is unaffected —
its ceilings are absolute and workload-independent — so correctness stays gated
throughout and only latency and throughput sensitivity dips.

The name has to be maintained by hand, and nothing can prove that a given edit
changed what a scenario measures. What the tests do hold is narrower and still
worth having: every scenario the workflow runs has a name, every row carries one,
and the gate reads it. An author who rewrites a scenario and leaves the name alone
gets the old behaviour for that series — but the name sits in the file they are
already editing to recalibrate the ceilings.

Cardinality is unchanged in practice. The workload is a function of the scenario,
and `commit` already opens a series per run.

The alternative considered was a per-series epoch date held in the gate: the
window starts at the date the current workload took effect. It needs no change to
the stored data, but it costs one extra query pair per metric per distinct
shortened window — measured at 3.1 s per query against the live store, roughly
thirty seconds a night — and it leaves the samples themselves ambiguous. A date
in a table says when something changed; a label on the sample says what it is.
