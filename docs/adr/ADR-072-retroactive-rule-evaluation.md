---
adr: 072
title: "Retroactive Rule Evaluation Over Local History"
status: Accepted
date: 2026-08-13
---

# ADR-072: Retroactive Rule Evaluation Over Local History

## Status

Accepted.

## Context

A rule arriving on a machine answers "is this happening now". The question an
operator asks in the same breath is "has this been happening" — and answering it
is what turns a newly written rule from a guess into something with evidence
behind it. Contoso installs `disk-slow` after DAL-WS-012's NVMe finally failed;
the useful reply is not "no drives are slow this second" but "four of your
machines have been drifting like that one was".

Answering it centrally means keeping every device's seconds somewhere they can be
queried. At the fleet this program is sized for that is a third storage path
holding roughly 5.5 GB per 48 h, and 48 h is all the reach it buys. The device
already holds the answer: the local store keeps a minute-by-minute rollup of
every vital, and evaluating a rule against it costs the endpoint a few seconds of
otherwise-idle processor.

That reach was, until now, arithmetic — the store's cap divided by its density —
and the arithmetic assumed the store holds nothing but today's vitals and that
the minute tier is never evicted. Neither is true. Eviction takes the globally
oldest block first, coarsest tier before finer at the same age, so the minutes go
*before* the seconds of the same age do.

## Decision

**A rule is re-run over local history, and history is only asked what it can
answer.** The store keeps one rollup per minute, so a rule about a shorter span
is reported as one history cannot answer rather than evaluated at the wrong
resolution. For the rest, which of the minute's stored readings is used follows
from the rule's own question:

| The rule asks | Read from the minute | Why |
|---|---|---|
| Did it *stay* past the line (a sustain)? | the least favourable reading | a finding then means every second of that minute was past it |
| Was the line *ever* crossed (no sustain)? | the peak | exactly answers it — the line was crossed if and only if the peak crossed it |
| The largest reading over a window | the peak | exact: the largest of a window is the largest of its minutes' largest |
| The mean over a window | the mean | exact for a full minute |

The direction is deliberately the one that cannot invent an episode. A retro scan
that reports something that did not happen costs more trust than one that misses
a marginal case, because the whole point of the scan is that its findings are
evidence.

**A finding is stamped with the minute it happened**, not the minute it was
found, and every finding of one scan carries one grouping key — so a scan folds
into a single incident per `(rule, scope)` rather than paging a queue once per
episode.

**A scan is scheduled around the machine.** It walks history in chunks bounded by
a number of stored readings, stands down between chunks for long enough to keep
its share of the processor small, and holds off entirely during maintenance,
while the host is busy, and while the host disk is filling. The disk threshold is
derived from the store's own footprint policy, so the scan always stands down
while the store is still keeping everything it was keeping.

**A scan is resumable from a durable cursor**, and re-reads a bounded window
before it on resume — long enough to rebuild everything the rule's state machine
carries — while suppressing findings from before the cursor. A stopped scan
therefore produces exactly the findings an uninterrupted one would.

**A rule is scanned once per version, not once per push.** The device remembers
the definition each scan covered, in full rather than as a digest. The server
pushes the whole ruleset on every reconnect, so triggering on the push would
re-raise every historical finding each time a flaky link came back; a digest that
changed with a toolchain would do the same after an agent upgrade.

**Findings spend the device's ordinary alert allowance.** They go through the one
bounded, rate-limited sink every producer writes to
([ADR-068](ADR-068-system-event-rules-and-the-edge-alert-sink.md)). A scan that
matches thousands of minutes is exactly the flood that ceiling exists for.

**The reach is measured, not divided.** A store is driven through the production
write path until its cap is evicting, then asked for the oldest minute it still
holds, at three cap sizes across a fourfold range; the shipped cap is
extrapolated from them with that measured linearity as the evidence. At the
vitals shape the fleet writes today — thirteen series at one reading a second on
a machine doing real work — the answer is **about seven months**, at a little over
two bytes per stored reading. That is months rather than the years the
central-versus-edge comparison originally claimed, and still two orders of
magnitude past the 48 h a central recorder would have kept, so the comparison
holds and the claim is restated as measured.

## Consequences

"Has this happened before" is answered without a central store of per-device
history, and without an on-demand pull.

A rule finer than a minute keeps working live and reports honestly that it cannot
be re-run. A device with no history reports an empty scope rather than a
completed scan — a machine enrolled this morning has not been checked back
through history it never had, and saying otherwise is the failure this whole
program exists to remove.

The reach follows the shape: a device that starts storing more series reaches
proportionally less far, so the measured number is only meaningful next to the
shape it was measured at, and the measurement reports its own density beside it.

The host's free disk is now read on a cadence and fed to the store, so its cap
backs off under host pressure as designed rather than only in principle.

## Alternatives considered

**A central flight recorder of every device's seconds.** A third storage path,
roughly 5.5 GB per 48 h at fleet scale, and only 48 h of reach — two orders of
magnitude short of what the device already holds for free.

**Pulling history on demand when a question is asked.** Puts a customer's
endpoint on the critical path of a console interaction, and answers only for the
device someone thought to ask about.

**Evaluating retroactively at the raw one-second tier.** Exact, but the raw tier
is evicted alongside the minutes and reaches back a fraction as far, so it would
answer a far narrower question at much greater cost.

**Reading the minute's mean for every rule.** One number for every question:
it damps exactly the spikes a peak rule exists to catch and cannot support a
sustained rule's claim that a whole minute was over the line.

**Marking findings with the time of the scan.** Destroys grouping, and makes a
freeze from three weeks ago look like it is happening now.
