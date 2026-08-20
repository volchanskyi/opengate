# Alerts and Rules

What OpenGate watches for on a customer's machines, and what it does when it
finds it. A rule states a condition; a device evaluates it against its own
readings; a breach becomes an alert carrying the evidence the device froze at the
time.

Everything an administrator can change about a shipped rule — retuning it,
labelling it, pacing its rollout, stopping it, and the budgets that bound it — is
in [Rule Administration](./Rule-Administration.md). What happens to an alert once
it exists is in [Investigations](./Investigations.md). The readings rules are
written against are in [Device Health](./Device-Health.md).


## Threshold alerts

Alongside the unsupervised anomaly detector, an agent evaluates a set of
declarative **threshold rules** locally every sample — a metric gauge, a
comparator, a fire threshold, a hysteresis clear boundary, and a sustain duration
([`alerts`](../../agent/crates/mesh-agent-core/src/alerts)). A breach must hold
continuously for its sustain duration before it fires (suppressing brief spikes),
then stays firing until the metric recovers past the clear boundary (suppressing
flapping around the threshold). A rule's `disk.used_percent` gauge reads the
fullest mount, so it fires for the volume that is filling; a host with no
measurable mount has no reading and no disk rule fires on it. The server delivers
each connecting agent's ruleset over a capability-gated `PushAlertRules` control
message ([`alert_rules.go`](../../server/internal/agentapi/alert_rules.go)),
assembled for the machine's own place in the tenancy ladder, so one customer's
rules never reach another's machines even inside a single tenant. A firing breach rides additively in an `AgentHealthSummary`,
which the server ingests as `opengate_edge_alert_breach` scoped to the resolved
tenant and charts on the Edge-Sentinel Soak dashboard. Delivery is
**investigation-aid only** — no auto-notify — until the false-positive soak; see
[ADR-053](../adr/ADR-053-edge-sentinel-threshold-alerts.md).

**What a rule can say.** Besides comparing the reading itself, a rule may compare
how fast it is changing, or its largest or mean value over a window, and may
require several dimensions at once. That covers the failures a single
instantaneous threshold cannot state: a disk whose service time is drifting
2 ms → 40 ms over a fortnight crosses no line on any given second, and a queue
28 deep at a healthy 3 ms is a nightly backup rather than a device in trouble —
it takes both sides together to tell them apart. The vocabulary is the vitals
dimensions, so a rule can only watch something the fleet actually collects, under
a canonical name or one of the aliases in
[Device Health](./Device-Health.md#the-vitals-contract). Every shape is bounded
and its cost computable from the rule's own text;
the wire fields and their limits are in
[Wire-Protocol](../architecture/Wire-Protocol.md#alert-rules-breaches-and-coverage).

**Where a rule comes from, and what a customer can change about it.** A rule has
three layers, separated by how mutable each one is.

Its *definition* — predicate, window, grouping key, the evidence its alerts
carry, and the numbers it ships with — is versioned YAML compiled into the server
from [`catalogue/`](../../server/internal/rules/catalogue). Definitions are
immutable per `(rule_id, version)`: loading refuses one whose meaning changed
without its version changing, checked against the digests committed in
`catalogue.lock`. That is what lets an alert raised last week still mean what it
meant then. Keeping definitions out of the database is also what makes the
program's highest-leverage gate possible — every rule's evaluation cost, the
readings it asks an endpoint to hold, is computed from its own text and bounded
in CI, per rule and across the whole pack, before it can reach an endpoint.

A customer's *bindings* live in Postgres and retune the numbers the rule declares
tunable, within the bounds that rule declares, validated on write. They resolve
down the tenancy ladder — machine, then site, then customer, then tenant, then
what shipped — using the ordering in
[`internal/settings`](../../server/internal/settings/settings.go), and each
parameter resolves on its own, so a customer-wide sustain window survives one
machine's retuned threshold. A binding may also carry a bounded tag selector with
an operator-set `precedence` breaking ties; across rungs the narrower one always
wins.

A rule's *rollout state* lives in Postgres too, because stopping a rule cannot
require a deploy. A customer with no row has not configured the rule and gets it
as shipped — absence is never read as "switched off". See
[Database](../architecture/Database.md) for the schema and
[ADR-071](../adr/ADR-071-rule-catalogue-bindings-and-durable-coverage.md) for the decision.

**Coverage: which machines a rule is actually watching.** Per rule, every device
in the fleet is exactly one thing, and together they always add up to the fleet:

| State | Meaning |
|---|---|
| `active` | The device is evaluating the rule |
| `unsupported` | The rule is producing no answer here: its metric is outside the vocabulary, its predicate outside the grammar's bounds, or the reading is not arriving (a kernel with no pressure accounting, a container whose disk counters are its neighbours', a disk that completed no I/O) |
| `throttled` | The rule cost this device more than its allowance, so the device stopped running it |
| `unknown` | The device has reported nothing — offline, or never seen |

`unsupported` is a first-class answer rather than an error path, because "no
kernel pressure information here" is a permanent platform gap and reads
completely differently from a machine that is merely quiet. A rule that is
answering nothing is reported that way whether the gap is permanent or passing —
claiming a rule watches a machine it produces nothing for is the failure coverage
exists to prevent, and a rule that starts answering reports itself active on its
next reading. Agents report their
own state per rule in `AgentHealthSummary.rule_coverage`
([`conn_coverage.go`](../../server/internal/agentapi/conn_coverage.go)).

They are not stored the same way, because they are not the same kind
of fact. `active`, `throttled` and `unknown` are liveness: they are *supposed* to
reset when the server loses sight of the fleet, so they live in memory. A device that
disconnects moves to `unknown` rather than vanishing from the count, and a server
restart is correct by construction rather than by a cleanup job — a stored
`active` would let a machine unplugged three weeks ago keep claiming it is being
watched. Being unable to evaluate a rule is durable: a containerized agent can
never read the kernel's per-host pressure accounting, so that is a standing hole
in an estate's monitoring, and it must answer the same after a deploy as before
one. That state is persisted, written through only when it changes — a
machine repeating itself costs no write at all — and a machine that can evaluate
the rule again has its row deleted rather than flipped, so nothing stored can go
stale. Deleting a machine takes its coverage rows with it.

**How far a rule has reached, and what stops it.** A curated rule that turns out
to be wrong is the one thing here that can degrade five thousand machines at
once, so a rule reaches a customer's estate in stages
([`stage.go`](../../server/internal/rules/stage.go)): a handful of machines, then a
tenth of them, then all of them. Each stage is held for a minimum before it may
grow, and the minimum is not what moves the rule — what moves it is the estate
having stayed quiet while it was held
([`gate.go`](../../server/internal/rules/gate.go)). A hit alert ceiling, a machine
that stopped the rule for costing too much, or a rule that failed to evaluate
sends it back to the population it was last quiet on, and restarts that stage's
clock, so a signal that comes and goes cannot ratchet a rule back up. A rule
already on its smallest population stops there rather than being pulled off the
machines watching it; ending it altogether is a kill, which is a person's
decision rather than a timer's.

Which machines a stage covers is worked out one machine at a time, from the
machine, the rule and the reach — no roster, no stored list of chosen devices.
That is what makes it the same answer on every server and across a restart, and
what makes growing a rollout only ever *add* machines. The floor of a handful
keeps a stage meaningful on a small estate, where a percentage of a dozen
machines is nobody; a customer's estate is counted for that, and only for the
customers who are mid-rollout.

A stop needs no deploy, because it is a row rather than a release
([`rule_rollout`](../architecture/Database.md)). Setting `kill` stops the rule on a connected
machine at the next push of its ruleset and on an offline one as it reconnects —
whichever comes first — because reconnecting re-resolves what the machine should
be running ([`alert_rules.go`](../../server/internal/agentapi/alert_rules.go)). A
kill outranks the stage: the canary that was proving the rule loses it too.

**What a rule may cost the machine running it.** The shipped pack is
cost-bounded in CI, but the endpoint is what pays, and a rule can reach one
without having come through that gate. So the machine enforces its own ceiling
over what a rule actually touched — not over what it declared — and stops any
rule that spends past it
([`evaluator`](../../agent/crates/mesh-agent-core/src/alerts/evaluator/mod.rs)). The
stop is per rule: one expensive rule must not silence the cheap ones, or a bad
rollout would become blanket blindness while still looking contained. It is also
hard — the rule is not retried on that machine until a *different* rule arrives,
so a flaky link re-pushing the same ruleset cannot spend the allowance again and
again. The device reports the rule as `throttled`, which is what a staged
rollout is watching for. The two ceilings are the same figure, so a rule the pack
allows can never trip the one on the endpoint.

**"Has this happened before?"** A rule arriving on a machine for the first time
is also re-run over the history that machine already holds
([`retro`](../../agent/crates/mesh-agent-core/src/alerts/retro/mod.rs)). The local
store keeps a minute-by-minute rollup of every vital going back further than any
central recorder of the fleet's seconds could afford, so the question is answered
on the endpoint and nothing is shipped anywhere to make it possible. Findings
come back as ordinary alerts marked as having come from history, each stamped
with the minute it **happened** — a freeze from three weeks ago stays three weeks
old, which is what lets a whole scan fold into one incident instead of a queue of
things that all appear to be happening at once.

Three properties keep it safe to run on a customer's endpoint:

- **A minute is only asked what a minute can answer.** A rule about a span
  shorter than one stored minute cannot be re-run at all and reports that,
  rather than being answered at the wrong resolution. For the rest, the reading
  taken from each minute is the one the rule's own question needs: a rule that
  has to *stay* over a line reads the minute's least favourable reading, so a
  finding means every second of that minute was over it, while a rule asking
  whether a line was *ever* crossed reads the minute's peak, which answers that
  exactly. The direction is deliberate — a scan that invents an episode is worse
  than one that misses a marginal one.
- **A scan is scheduled around the machine, not around the queue.** It walks
  history in chunks of a fixed number of stored readings, stands down between
  them to keep its share of the processor small, and suspends entirely during
  maintenance, while the host is busy, and while the host disk is filling —
  standing down before the store itself starts trading history away for space.
  It records where it got to, so a busy host or an agent restart costs no
  repeated and no skipped finding.
- **A scan spends the same alert allowance as anything else.** Findings go
  through the one bounded, rate-limited sink every producer writes to, so a rule
  that turns out to match thousands of minutes cannot flood a customer's queue
  with them.

A rule is scanned once per **version**, not once per push: the server pushes the
whole ruleset on every reconnect, and re-running history each time would re-raise
every historical finding whenever a flaky link came back. A retuned threshold is
a different version and is scanned afresh. See
[ADR-072](../adr/ADR-072-retroactive-rule-evaluation.md) for the decision.

**How far back that reaches** is measured rather than divided out of the store's
cap, in
[`reach_test.rs`](../../agent/crates/mesh-agent-core/tests/reach_test.rs): a store
is driven through the production write path until its cap is evicting, then asked
for the oldest minute it still holds, at three cap sizes across a fourfold range
so the extrapolation to the shipped cap rests on measured linearity. At the
vitals shape the fleet writes today — thirteen series at one reading a second on
a machine doing real work — that is **about seven months**, at a little over two
bytes per stored reading. The number moves with the shape: a device storing more
series reaches proportionally less far, which is why the test reports the density
it measured beside the reach.

## System-event rules

Some failures never cross a threshold, because nothing about them is a number.
A task stuck for two minutes, memory reclaimed by killing a process, a disk that
stopped answering its bus, a processor slowing itself down to survive its own
heat — the machine reports every one of these about itself, in words, in its own
log. A curated pack of four Linux rules reads them from the systemd journal
([`event.rs`](../../agent/crates/mesh-agent-core/src/alerts/event.rs)), and a fifth
rule counts something no single record says: one service producing errors over
and over for a day.

Each rule matches on alternatives and, more importantly, on **exclusions**. Every
subsystem that reports a failure also reports its recovery, usually naming the
same component in nearly the same words — a disk that resets its link announces
the link coming back up, a throttled core announces its temperature returning to
normal. A rule without exclusions looks correct until it pages someone for a
machine that just got better, so the fixture corpus carries a near-miss per rule
and the pack is tested against both halves.

**The reader is a bounded on-demand read, not a stream**, so the watch is a poll
whose window reaches back further than the interval between polls and therefore
re-presents records it has already seen. A cursor is what makes that free
([`event_watch.rs`](../../agent/crates/mesh-agent/src/event_watch.rs)): a record
newer than the cursor fires, a record at the cursor's instant fires only if it
was not already answered for there, and a record older than it never fires. The
last of those is a deliberate trade — a record arriving late is lost rather than
duplicated, because an alert delivered twice costs more trust than one delivered
never. What the poll fetches is bounded by the level floor the pack states about
itself, so a rule watching something less severe widens the read by existing
rather than by anyone remembering to widen it.

**A poll that comes back at the reader's line cap saw only the newest end of its
window.** How many records fell off the old end is not knowable, so the poll is
counted as an event in itself and no number of lost records is invented.
Alongside it the watch counts records it could not place in time and services the
tracking cap turned away. A record a curated rule already explained does not also
feed the per-service count: reporting one event twice, the second time under a
vaguer name, is worse than reporting it once.

Maintenance mode **suppresses** the window rather than deferring it. An admin
rebooting a host produces exactly the records this pack matches, so holding them
until maintenance ends would page someone for the maintenance itself.

Every alert is redacted at the edge before it exists
([ADR-049](../adr/ADR-049-edge-sentinel-raw-log-privacy.md)), since an alert is the
one path that lifts a log line off a host outside the Logs pane. Alerts from
every edge producer land in one bounded per-device sink
([`sink.rs`](../../agent/crates/mesh-agent-core/src/alerts/sink.rs)) that drops its
**oldest** entry when full — the newest alert describes what the device is doing
now — and admits no more than the per-machine hourly ceiling an administrator set
for that customer, so one host in a loop cannot drown the detection of every
other host. The ceilings, their code-side maxima, and how a change reaches a
running sink are in
[Rule Administration](./Rule-Administration.md#alert-limits). Both limits lose
alerts by design and both count every alert they cost; a suppression nobody counts
is indistinguishable from a quiet device.

## Ranking what broke

An alert says what crossed a line. The question straight after it is what else
moved at the same time, and the device is the only place that can answer with
detail: it keeps 1 s readings locally, while what reaches the centre is a 60 s
average per dimension, in which a ten-second I/O collapse is a bump.

So the agent ranks its own dimensions over the event window
([`correlate`](../../agent/crates/mesh-agent-core/src/correlate)) against the
stretch immediately before it, and the ranking travels with the alert. Three
signals blend into one score in `[0, 1]`: how much a dimension's distribution
changed shape (a two-sample Kolmogorov–Smirnov statistic), how many readings in
the event window fell outside the baseline's normal band, and how far the mean
moved measured against the baseline's own scale. The third is what stops a
service time that went from 0.40 ms to 0.44 ms outranking one that went from
0.4 ms to 40 ms — the first two saturate on any clean separation, however small.

Degenerate windows are answered with a number rather than a NaN: a dimension
with fewer than two readings on either side is left out instead of scored from
nothing, a gauge that read the same value all hour has no band so any different
reading counts, and a reading that is not a real number is dropped where it
enters. Ties are broken by shape change and then by label, so the same readings
always produce the same order.

The read is an MVCC snapshot of the local store, so a correlation running while
the sampler writes neither blocks ingestion nor sees a moving target. Every run
is bounded three ways — how many dimensions are examined, how many readings each
window carries, and how long the whole thing may take — because the moment this
code runs is the moment the machine is already in trouble.

## What an alert carries

An alert is self-contained. There is no path for asking the device afterwards, so
everything behind a finding is attached when it fires or it exists nowhere: the
ranked dimensions the device's own correlation produced, a few of their series
around the event, the processes running at the instant, and a bounded sample of
redacted host log lines. The composition is fixed rather than best-effort — two
incidents a week apart are only comparable if they were assembled the same way —
and when the whole thing is over its size cap it gives up its least valuable
parts in a stated order and says it did. The exact parts, their bounds, the
compression codec and the truncation order are in
[Wire Protocol](../architecture/Wire-Protocol.md#alerts-and-their-evidence).

Every free-text field is redacted on the device before the alert exists, because
an alert is the one path that lifts a log line off a host outside the logs pane;
see [Endpoint Logs](./Endpoint-Logs.md).

## How an alert reaches a person

An alert is not a message anybody is sent. Every stored alert is folded into an
incident in the same write that stores it, and the incident is what a technician
opens — from the triage queue at `/investigations`, or from the strip of open
incidents on the machine's own page. Grouping, the queue, the room and the
lifecycle are in [Investigations](./Investigations.md).

Browser push notifications carry device and session lifecycle events —
a machine coming online or going offline, a remote session starting or ending —
and no alert path; see [Fleet and Devices](./Fleet-and-Devices.md).
