---
number: 117
title: The profile offers the load and names the window
status: Accepted
date: 2026-09-10
---

# ADR-117 — The profile offers the load and names the window

## Context

[ADR-101](ADR-101-one-measurement-one-limit-one-file.md) made the profile the
only home for the numbers a night is judged by. It was not the home of the load
that produced them.

Two of every profile's phase numbers are technician-side —
`operator_arrivals_per_second` and `sessions`. The machine-side harness reads
the profile and can offer neither: it drives machines, and a journey and a
session are the browser's side of the wire. The browser-side generator, which
could offer both, had never seen a profile. So each scenario carried a shape of
its own — twenty virtual users, ninety seconds, a second and a half of sleep —
and a profile could declare fifteen journeys a second while the run offered
something else entirely, with no number anywhere saying the two disagreed. What
varied the load was whichever file somebody happened to edit.

The scaling sweep is where that cost the most. Its stated subject is holding the
load constant while varying the processors, and no browser-side generator ran on
its venue at all — so what it varied processors against was five hundred
machines arriving, which is the cheapest thing the server does. Both nights on
record read flat from one processor upwards, which is the honest answer to the
question it was actually asking.

Two smaller faults sat beside it. The scenarios held *users*, not an arrival
rate: each waits for its own reply before asking again, so a server that has
slowed is offered less work and the latency it reports understates the damage.
And every percentile spanned the whole run — a thirty-second climb, a minute at
the load, a thirty-second wind-down — so the figure described a mixture of three
systems, and that mixture moved whenever the climb took a different share of the
run.

## Decision

**The profile's walk is projected into the generator, and the generator builds
its executors from it.**

[`profile_phases`](../../scripts/lib/loadtest-profile.sh) reads the walk;
[`loadtest-k6-run.sh`](../../scripts/loadtest-k6-run.sh) hands it over; the
generator turns each phase into one scenario, tagged with that phase's name. A
run with no profile is refused rather than defaulted — a default is the
undeclared shape this closes, wearing a different name.

**Journeys arrive at a rate.** The phases that declare arrivals become
`constant-arrival-rate` scenarios. Sessions stay user-based, because one session
per virtual user is what *twenty sessions are open* means; an arrival rate there
would describe sessions being opened per second, which is a different question.
What the open loop costs is a second way to be wrong — a generator that cannot
keep the rate offers less, latency stays flat, and the night reports a healthy
system nobody finished asking — so `dropped_iterations` becomes a series the
profiles hold to a limit.

**A profile names the phase its numbers are taken over.** One phase carries
`measured: true`; the schema refuses two, because percentiles pooled across two
loads describe neither. The generator names that phase's sub-metric in a
threshold, which is what puts it in the summary export, and both the canonical
row and the bundle read that sub-metric where it exists. Throughput is
deliberately not windowed: the exporter divides a sub-metric's count by the
whole run, so a windowed rate reads a phase as slower than it was.

**A generator joins the walk where the walk is.** The harness announces when it
started walking; the browser-side generators, which cannot start until the
estate they read is filed, subtract what has already gone. One that started the
shape again from its beginning would be a phase behind for the rest of the
night, holding its steady window open past the drain and publishing a percentile
taken partly against a fleet that had already left.

**The sweep venue gains a generator.** The scaling job runs the browser-side
scenario beside its fleet, folds the journeys into the leg's own bundle, and
[`perf-scaling-curve.sh`](../../scripts/perf-scaling-curve.sh) reads them — a
rung with no technician reading is refused, because it is a rung where the thing
being held constant was not offered at all.

## Consequences

Every browser-side series is a reading of different work, so each takes a new
`workload_name` ([ADR-092](ADR-092-a-trend-series-carries-the-workload-that-produced-it.md))
and compares against itself. Four changes moved at once — the load is the
profile's, it arrives at a rate, each technician presents an address of its own
([ADR-116](ADR-116-a-presented-address-is-believed-from-a-named-proxy.md)), and
the percentile is taken over one phase — and a window median spanning both sides
of that would be a comparison nobody could interpret.

The three browser-side scenarios now run at the same time rather than one after
another, because the profile describes one night rather than one scenario's
night. Run in turn they would each walk the whole shape, which is three nights
end to end and none of them the one the profile declares.

`operator_arrivals_per_second` is the rate *each* browser-side generator offers,
so a night's total technician load is that rate times the generators running. It
is stated here because the profile carries one number and two scenarios read it.

The load the everyday profile declares is smaller than the shape it replaced —
five journeys a second where the old fixed shape happened to offer more. That is
the point rather than a regression: the number is now one somebody chose and can
argue about, and the families that ask for a heavy technician load ask for it in
their own profiles, up to a hundred and sixty journeys a second at the top of
the ladder.
