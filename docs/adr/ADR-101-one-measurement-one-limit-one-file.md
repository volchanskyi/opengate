---
number: 101
title: One measurement, one limit, one file
status: Accepted
date: 2026-09-06
---

# ADR-101 — One measurement, one limit, one file

## Context

Two separate things decided whether a night's load run was acceptable, and both
held numbers for the same measurements.

[`loadtest-regression-check.sh`](../../scripts/loadtest-regression-check.sh) ran
after every night. It compared tonight against a typical night from the last
fortnight, and it also held a set of absolute limits keyed by
`source/scenario/phase`, as a floor under the case the window comparison cannot
see: a product that degrades slowly drags its own window median down with it and
nothing ever fires.

Every profile in [`load/profiles/`](../../load/profiles/) also declared limits,
keyed by exactly the same triple. **Nothing read them.**
[`profile.go`](../../server/tests/loadtest/profile.go) validated their shape and
no code consumed one, so every number in all seven profiles was decoration —
including the rows marked `blocking: true`.

For `k6/api-baseline/http` `latency_p95_ms` that came to two numbers: **200** in
the script, enforced, and **100** in `normal.yaml`, read by nothing. Making the
profile's numbers live without settling that gives one measurement two enforced
limits in two files, where an edit to either does not do what it says.

Two further faults sat under it:

- **The limits named measurements their venue cannot produce.** Twelve of the
  sixteen name a browser-side series. `Classify` runs inside the machine-side
  harness, whose bundle holds phases and machines and no browser-side row at
  all — so from there they were unreachable by construction. Worse,
  `scaling.yaml` and `volume.yaml` run on a throwaway machine where **no
  browser-side generator runs at all**, and each carried a limit on one.
- **`Phase.Sessions` drove nothing.** `scaling.yaml`'s `sessions: 5` ran zero
  sessions, and nothing anywhere said so.

## Decision

**The profile is the only home for the numbers. The script keeps only the
method.**

### A measurement carries one limit that fails a night, and any number of marks that only report

These are different statements and collapsing them loses whichever is dropped:

- a **limit** is loose on purpose — the floor under the window comparison's
  blind spot;
- a **mark** is a target somebody has committed to and is watching before
  enforcing, because a number that fires on the measurement's own noise teaches
  everyone to ignore it.

So `k6/api-baseline/http` `latency_p95_ms` keeps both: 200 that fails the night,
100 that reports. `Profile.validateGates` refuses a second blocking gate on one
measurement **per direction** — a ceiling and a floor answer different questions
and are not duplicates of each other.

### Every measurement the extraction produces carries a decision

The limits the profile took over had a catch-all: a series nobody listed was held
to a default automatically. A profile has no catch-all, so consolidating could
have **deleted protection while looking like tidying up**.
[`loadtest-gate-series.test.sh`](../../scripts/tests/loadtest-gate-series.test.sh)
now checks both directions — every measurement the summarizer emits is either
limited or named in the profile's `ungated:` list with a stated reason, and every
decision names a measurement that actually arrives.

Writing that inventory out found two real things. The test's own k6 fixture had
never carried the journey metrics, so three limits on the technician journeys had
never been exercised by anything. And three journey `latency_p50_ms` limits,
inherited from the catch-all at 1000 ms while their tails were held to 300, 500
and 1000, could never fire — half the requests are at or below the figure the
95th percentile reports. Those three are now declared unlimited, with that as
their reason.

### The limits are read where both halves of the night exist

[`loadtest-gate-check.sh`](../../scripts/loadtest-gate-check.sh) runs in the
publish step, over the canonical rows, after the browser-side and machine-side
rows have been joined. It reads no history at all, so it is exactly as awake on
the first night of a series as on the hundredth — which is what makes it the
right owner of the absolute limits, given that a renamed workload resets the
fortnight window by design ([ADR-092](ADR-092-a-trend-series-carries-the-workload-that-produced-it.md)).

A limit whose measurement never arrived **fails the night**. It reads as a
passing limit forever otherwise, which is the same false green as a step that
reports success for work it was refused
([`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md)). A profile
declaring no limits at all is refused for the same reason: it and a night that
cleared everything produce the same output and are opposite facts.

A breach is a finding about the system, so it fails the night and the rows still
enter the trend — the treatment a leaking target already gets
([ADR-094](ADR-094-a-run-records-what-its-target-was-holding.md)).

### A limit names a measurement its venue can produce

[`loadtest-gate-venue.test.sh`](../../scripts/tests/loadtest-gate-venue.test.sh)
reads which profile each workflow names and which generators it starts, and
refuses a limit whose source that venue does not run. `scaling.yaml` and
`volume.yaml` now limit the machine side, which is what a throwaway machine
produces. Because the check reads the workflows rather than a list, a venue that
gains a browser-side leg makes those limits legal without the check being edited.

### A technician-side declaration travels as an offer, never as an achievement

`sessions` is the browser's side of the wire, like `operator_arrivals_per_second`
beside it. The machine-side harness carries both into the bundle as offers, with
their achieved halves absent, so a profile asking for five sessions and running
zero says so instead of saying nothing.

## Consequences

The regression check no longer fails a night on an absolute limit — the gate
check does, in a different job. Both red the night. The regression check's own
tests now assert the opposite of what they used to on a cold window: that it
compares against nothing and says nothing, which is the honest report when there
is no window to compare against.

The publish job reads YAML through `python3` and PyYAML, which the runner image
carries. [`loadtest-profile.sh`](../../scripts/lib/loadtest-profile.sh) fails
loudly when it cannot read rather than answering — a gate that says yes when it
could not ask is worse here than anywhere, because the caller is deciding whether
a night's numbers were acceptable.

`normal.yaml` grew from 8 gate rows to 38 plus 3 declared exemptions. That is the
inventory, and its length is the point: it is the first time the set of numbers
this project judges a night by has been written down in one place.
