---
adr: 076
title: "Aggregate Platform Metrics, and the Alert Rate as a Measured Gate"
status: Accepted
date: 2026-08-15
---

# ADR-076: Aggregate Platform Metrics, and the Alert Rate as a Measured Gate

## Status

Accepted.

## Context

[ADR-074](ADR-074-alert-store-accounted-ingest-and-the-erasure-cascade.md) and
[ADR-075](ADR-075-incident-grouping-lifecycle-and-auto-resolve.md) built the
machinery that turns what a machine reports into a room a person works in. What
neither gave is a way to see the machinery itself behaving: whether a curated
rule that has just started reaching an estate is raising what it should, whether
a customer's hourly ceiling has started refusing detection, whether the triage
queue is draining, and how much of the fleet each rule is actually watching.

Two separate problems sit inside that.

**A rule that is valid, affordable and wrong is the one thing that can degrade
every estate at once.** The closed grammar bounds what a rule can say and the CI
cost gate bounds what it costs an endpoint, but neither has an opinion about
whether the numbers on it are right. Contoso's `disk-slow` tuned two milliseconds
too tight is a well-formed, cheap rule that raises on 380 machines through the
nightly backup window. The staged rollout
([ADR-073](ADR-073-staged-rule-rollout-and-the-endpoint-budget.md)) is what
contains that, and it reads its signals from the estate — but a person watching a
rollout advance has, until now, had nothing to look at.

**A number three decisions rest on has never been measured.** The alert rate is
carried as 0.2 alerts per device per day, and it is an estimate. The evidence
volume projected for a year is derived from it. The customer hourly ceiling
([`OrganizationHourlyCeiling`](../../server/internal/alerts/types.go)) was set as
roughly twelve times it — twelve times a rate nobody has observed. And it is one
of the conditions under which the deferred question of per-device alert series
would be reopened. An assumption carrying that much weight is the shape of defect
that surfaces as an incident rather than as a finding.

## Decision

**Five aggregate series on the existing `/metrics`, and every one of them is
O(rules).** `opengate_alerts_created_total{rule_id}`,
`opengate_alerts_suppressed_total{reason}`, `opengate_alerts_open`,
`opengate_incidents_open{status}` and `opengate_rule_coverage{rule_id,state}`
([`internal/metrics/investigations.go`](../../server/internal/metrics/investigations.go)).
They are platform meta-monitoring, in the platform tool: Grafana is a
ClusterIP service with no ingress anywhere, and product views read the API.

**Cardinality is the binding constraint, and it is bounded by construction
rather than by care.** No series here carries a tenant, a customer, a machine or
an incident. A rule pack is a handful of entries fixed for a release; a fleet is
however many machines every customer between them runs. One entity label turns
O(rules) into O(rules × estate) and makes the platform's own monitoring the
largest cardinality source in the system it exists to watch — growing with
exactly the dimension nobody is watching it for. The per-device picture stays at
the edge, where the detail already is.

**The `rule_id` label is bounded by the shipped catalogue, not by the
endpoint.** Rule ids travel to the agent and come back on alerts and coverage
reports, so an unbounded label there would let a device decide this server's
cardinality. The catalogue's ids are declared at start-up; anything outside them
folds into one catch-all value. That is the same bound the WS-19 breach path
applies to its `metric` label.

**Every value of every closed vocabulary is exported, zero-valued ones
included.** A missing series reads as "no data" on a dashboard, which is not the
same answer as "none open" — and the two are indistinguishable exactly when
somebody is checking whether a rollout raised anything. So a rule that has never
fired reads zero, a status nothing is open in reads zero, and all four coverage
states are exported per rule so they visibly add up to the fleet.

**The two gauges are refreshed on a timer, not computed in the collector.** They
are counts over tables that only grow, and `/metrics` is scraped by more than one
thing. Each is one aggregate per interval — the open-work read is a single
statement joining open incidents to their alerts through the incident index, so
its cost tracks the queue rather than the table; the coverage read counts the
fleet and the blind spots in one pass, because a blind-spot count read a moment
apart from the fleet it is a fraction of reports a share nobody's estate was ever
in. Both run admin-scoped over every tenant, for the same reason the stale-room
sweep does: a triage queue in a tenant nobody is currently serving requests for
is still a triage queue, and these series carry no tenant to confine them to.

**A read that fails leaves the previous answer standing.** A database that is
briefly unreachable is not an empty triage queue and not a fleet where every
machine is unknown; both of those are real, alarming states, and a gauge told
they are the same would raise one for the other.

**Only a stored alert counts as created.** A reconnect replaying an alert already
held changed nothing, and a refusal past the ceiling stored nothing at all.
Counting either inflates the very rate the ceilings are sized against.

**The alert rate becomes a measured gate, and the pack does not advance past its
canary stage until a real population has produced one.** The counters above make
it observable; the measurement is
`increase(opengate_alerts_created_total[24h])` over the fleet size, and the fleet
size is read off the coverage gauge whose four states always sum to it. The
procedure and the decision that follows from each outcome are written into
[Monitoring.md](../Monitoring.md).

**A figure from a synthetic soak is a figure about the harness, and is reported
as one.** The soak fleet is a single device; five canary machines on a
one-device fleet cannot produce an estate-scale rate. If an earlier signal is
wanted, a synthetic run is acceptable only when reported as a harness figure
naming its rule pack and fixture — never as the fleet rate. Publishing an assumed
number as a measured one is the failure this gate exists to prevent, so it is not
committed by the gate itself.

## Consequences

A bad rollout is visible while it is still a canary: one `rule_id` climbing while
the rest hold steady is a rule whose thresholds need moving, and a rise across
the pack is a rollout that reached more machines than it was staged for. Two
Grafana alerts watch it — one on the projected per-device rate, one on any
suppression at all, since suppression is detection being refused rather than
noise being filtered.

Row growth stays observable while retention is deferred: a day of
`alerts_created_total` projects the year the evidence-volume estimate is made of,
and the same expression is the Q12 measurement.

The counters reset when the process restarts, and that is left alone. `increase()`
tolerates it, and a persisted counter would trade a query-layer detail for a
storage one.

Three numbers stay provisional until the measurement lands: the yearly evidence
projection, the customer hourly ceiling, and the trigger for reopening per-device
alert series. Each has a stated response if the measured rate comes back high —
raise the ceilings and pull the retention sweep forward, tighten the curated
thresholds or ship a smaller pack, or strengthen grouping so the incident count
stays usable at the measured alert count — and choosing between them is an owner
decision, not this record's.

## Alternatives considered

**Per-rule series in VictoriaMetrics alongside the per-device edge series.**
Rejected: these are fleet aggregates about the platform, not readings about a
machine, and the existing per-device alert-series question is deliberately still
open. Putting them in the time-series store would entangle the two.

**Computing the gauges inside the Prometheus collector.** Rejected: a full
aggregate over growing tables on every scrape of an endpoint several things
scrape, with no bound on how often that happens.

**A `tenant_id` or `organization_id` label so a customer's queue is visible in
Grafana.** Rejected outright — it is the one change that breaks the cardinality
property these series are built on, and the customer-facing view of a customer's
queue is the API, which is already scoped to them.

**Publishing 0.2 alerts/device/day as measured once the counters shipped.**
Rejected: that is precisely the failure of citing an assumed number as an
observed one. The counters make it measurable; a real population makes it
measured.
