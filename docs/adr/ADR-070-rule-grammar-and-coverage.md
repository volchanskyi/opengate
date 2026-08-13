---
adr: 070
title: The Alert-Rule Grammar, Metric Aliasing, and Explicit Coverage
status: Accepted
date: 2026-08-12
---

# ADR-070: The Alert-Rule Grammar, Metric Aliasing, and Explicit Coverage

## Status

Accepted.

## Context

The shipped rule ([ADR-053](ADR-053-edge-sentinel-threshold-alerts.md)) is four
fields — a metric, a comparator, a threshold, a clear boundary — plus a sustain
duration, over three metric names. Two things make that an inadequate base for a
curated rule pack.

**It names dimensions that no longer exist.** The vitals contract
([ADR-065](ADR-065-vitals-contract-cadence-extrema-and-bounded-dims.md)) calls
them `mem.used_percent` and `disk.used_percent`; the rules still say `mem.used`
and `disk.used`. Rules are already pushed to the fleet under the old names.

**It cannot state the failures the fleet actually has.** Every rule it can
express is one instantaneous reading crossing one line. DAL-WS-012's NVMe wears
out over a fortnight, its service time drifting 2 ms → 40 ms, and no second of
that crosses a line anyone would set in advance — a line low enough to catch the
drift fires on every backup window. FS01's 02:00 backup runs 28 I/Os deep at a
healthy 3 ms; a device in trouble runs 28 deep at 40 ms. One number cannot
separate them, and neither can two rules that fire independently.

The constraint that shapes every answer below: an RMM agent that executes
server-supplied code is a supply-chain weapon aimed at every customer estate. So
the grammar may grow, but not into a language.

## Decision

### Rules stay data in a closed, cost-boundable grammar

Three additions, and no more: **rate of change**, **window aggregate**
(`max` / `mean`), and **cross-dimension conjunction**. Existing comparators,
`sustain_secs` and hysteresis `clear` are unchanged, and a rule that declares
none of the new fields is exactly the rule the fleet already runs.

The shape is a `predicate` naming how the compared number is derived, a
`window_secs` the predicate spans, and an `all` list of extra conditions that
must hold at the same instant — each carrying its own metric, comparator,
boundaries, predicate and window
([`control.rs`](../../agent/crates/mesh-protocol/src/control.rs)).

Every predicate's cost is a function of the rule's own declared fields:
[`rule_cost`](../../agent/crates/mesh-agent-core/src/alerts/evaluator.rs) answers
how many readings a rule retains and may touch, monotone in the window and summed
over the conditions. **A predicate whose cost cannot be computed statically is
one the grammar cannot express** — which is why the window and the term count are
bounded constants rather than whatever a machine turns out to survive, and why an
ill-formed shape (a window past the bound, a windowed predicate with no window,
an instant predicate carrying a window it would silently ignore, more terms than
allowed) is refused by name rather than attempted. One shape, one meaning: a
field the evaluator ignores is a rule nobody can predict from its own text.

Rejected: an expression language, a scripting hook, WASM. Each moves the
program's highest-impact gate — knowing what a rule costs before it reaches
5 000 endpoints — from a build-time computation to a runtime hope.

### Conjunction is one rule, not two

The alternative is two rules and a correlation somewhere downstream. That is
worse in the place it matters: two rules fire independently, so a machine with a
deep queue *or* a slow disk pages twice for one condition, and the operator
reading the queue alert has no way to know the service-time alert exists. A
conjunction states "these together are the problem" once, in the rule, where the
operator wrote it and can read it back.

Its hysteresis follows from the same reading: the situation is over as soon as
**any one side** has genuinely recovered past its own clear boundary. With a
single condition that is exactly the hysteresis the rule has always had.

### The vitals names are canonical; the old names are aliases

`mem.used` and `disk.used` resolve to `mem.used_percent` and
`disk.used_percent`, so a rule already on the fleet keeps watching the same
reading across the upgrade. A breach always names the **canonical** metric,
whatever the rule was written in, so one reading is never recorded under two
names — and the server resolves the same way on the way in, so an agent that
predates the rename and one that follows it write the same series.

The vocabulary extends to the full vitals set, `disk.await_ms` and
`disk.queue_depth` included; without them the wear-out case is collectable but
not alertable. **A rule naming anything else never fires and is counted
`unsupported`, never silently skipped.**

Alias resolution lives in **one place per side**
([Rust](../../agent/crates/mesh-protocol/src/control.rs),
[Go](../../server/internal/protocol/rules.go)) and the two are pinned together by
the `go_control_push_alert_rules.bin` fixture: the server generates it from its
own vocabulary, and the agent decodes it asserting that what it resolved is
exactly its own set. A name either side adds alone fails that test.

### Coverage is a state per device per rule, and the three states add up

Per rule, every device is exactly one of `active` (evaluating), `unsupported`
(the rule is producing no answer here) or `unknown` (reported nothing).
`active + unsupported + unknown == fleet size`, always.

**`unsupported` is a first-class state, not an error path.** The temptation is to
treat "this kernel publishes no pressure information" as a failed evaluation,
which produces `unknown` and hides a permanent platform gap behind a
transient-looking label. A rule quietly evaluating on half an estate while
reading as healthy is precisely the failure class this program exists to
eliminate.

`unsupported` covers a permanent gap (a metric outside the vocabulary, a
predicate outside the grammar's bounds, a kernel with no pressure accounting, a
container whose disk counters are its neighbours') and a passing one (a disk that
completed no I/O has no service time, because 0 ms would read as instantaneous),
because one sample cannot tell those apart. That is deliberately the conservative
direction: claiming a rule watches a machine it produces nothing for is the
failure coverage exists to prevent, and a rule that starts answering reports
itself `active` on its next reading.

### Coverage rides the summary, and has no table

Coverage travels as an additive `rule_coverage[]` on `AgentHealthSummary`,
exactly as breaches already do — it is small, per-device, and already on its way.
It is omitted entirely when there is nothing to say, which is also the shape an
agent that predates coverage sends; the server reads both as this device having
reported nothing. A calm machine emits no breach summary at all, so coverage
rides the periodic anomaly summary too, or a rule watching nothing on a quiet
fleet would be invisible.

**No `rule_coverage` table.** Coverage is a liveness view, not a history: what a
rule is doing on a machine is knowable only while that machine is connected, and
a device that has said nothing since the server started is exactly what `unknown`
means. Holding reported states in memory per connected agent and deriving
`unknown` as `fleet size − reported`
([`conn_coverage.go`](../../server/internal/agentapi/conn_coverage.go)) is
therefore correct across a restart by construction rather than by a cleanup job,
and a device that disconnects moves to `unknown` rather than vanishing from the
count.

## Consequences

- The curated pack can state the failures that motivated it: a drifting service
  time as a rate or a windowed mean, a within-minute freeze as a windowed
  maximum, and slow-and-queued as one rule rather than two.
- A partial window is **not enough data**, not a fire. A rule warming up is
  `active` — evaluating — not `unsupported`; firing on a partial window would
  make every agent restart page someone.
- Coverage counts are held in memory and exposed per rule; the Prometheus export
  and the operator-facing surface are separate work. Should coverage ever need to
  survive a restart, it becomes a column set on the rules schema — an additive
  change, because nothing here depends on its absence.
- Coverage is agent input, so the rules one device may report on are capped and
  their ids sanitized on the same path as breach rule ids: a device cannot make
  the server hold more rule ids than it could ever be pushed.
- A summary carrying only coverage produced state, so it is not counted as a
  discarded message — the telemetry ledger
  ([ADR-044](ADR-044-edge-sentinel-server-telemetry-ingest.md)) keeps balancing.
