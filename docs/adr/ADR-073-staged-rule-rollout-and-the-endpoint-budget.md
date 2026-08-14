---
adr: 073
title: "Staged Rule Rollout, the Kill Switch, and the Endpoint's Own Budget"
status: Accepted
date: 2026-08-13
---

# ADR-073: Staged Rule Rollout, the Kill Switch, and the Endpoint's Own Budget

## Status

Accepted.

## Context

One curated rule reaches every machine of every customer that has it. That makes
"a bad curated rule degrades five thousand endpoints" the highest-impact thing
this program can do to a customer, and the only one whose blast radius is the
whole fleet at once. Two mitigations already exist: the grammar is closed and
declarative ([ADR-070](ADR-070-rule-grammar-and-coverage.md)), and every rule's
cost is computed and bounded in CI before the pack ships
([ADR-071](ADR-071-rule-catalogue-bindings-and-durable-coverage.md)).

Neither covers the case where a rule is *valid, affordable and wrong*. Contoso's
`disk-slow` ships tuned two milliseconds too tight; every one of their 380
machines starts raising it during the nightly backup window, and the first
anyone knows is a queue nobody can work through by morning. Nothing in the
grammar or the cost gate has been violated.

Neither covers the reverse case either: a rule reaching an endpoint without
having passed the cost gate at all — an operator's own rule, a provider that is
not the catalogue, an agent running against a server that ships a different pack.

## Decision

**A rule reaches a customer's estate in stages, and each stage has to be earned.**

| Stage | Population | Held for | May grow when |
|---|---|---|---|
| Canary | the larger of five machines and 1 % | 1 h | no alert ceiling was hit, no machine throttled the rule, no evaluation failed |
| Staged | 10 % | 6 h | the same |
| Full | the whole estate | — | — |

The hold is a **minimum, not a trigger**. Time alone never moves a rule; what
moves it is the estate having stayed quiet while the stage was held. Any of the
three signals sends the rule back to the population it was last quiet on and
restarts that stage's clock — so a signal that comes and goes cannot ratchet a
rule back up, because the stage it fell back to has to be earned again from the
moment it fell back. A rule already on its smallest population **halts** there
rather than being pulled off the machines watching it; ending it altogether is a
kill, which is a person's decision rather than a timer's.

**The signals arrive through a port, never a guess.** The stage machine reads no
counter itself. A gate reading an approximated signal would be worse than no gate
at all, because it would look like protection.

**Membership is computed per machine, not stored.** Whether a machine is in a
stage is a function of the machine, the rule and the reach — no roster, no list
of chosen devices. That makes it the same answer on every server and across a
restart, and makes growing a rollout only ever *add* machines: a machine that
dropped out as the reach rose would have the rule installed, removed and
installed again across one afternoon. The rule id is part of the function, so two
rules being tried at once do not both land on the same handful of endpoints.

**The canary has a floor of five machines, bounded by the fleet, and it holds for
every partial stage.** One percent of a dental practice's twelve machines is
nobody, and a stage that reached nobody would advance on an hour of silence that
proved nothing. Bounding it by the fleet keeps it honest on an estate of three;
holding it across stages keeps a rollout from shrinking as it moves forward. The
estate is counted for this, and only for customers who are mid-rollout — which is
nobody, until somebody stages something.

**Stopping a rule is a row, not a release.** `kill` on `rule_rollout` stops the
rule on a connected machine at the next push of its ruleset and on an offline one
as it reconnects, whichever comes first, because reconnecting re-resolves what
the machine should be running. No deploy is on the critical path for stopping a
rule that is degrading customer machines. A kill outranks the stage: the canary
that was proving the rule loses it too.

**The endpoint enforces its own budget, over what a rule actually touched.** A
rule may touch the same number of readings per second that the pack's CI gate
bounds a rule at, measured over a minute. Past that the machine stops running
that rule. The stop is:

- **per rule** — one expensive rule must not silence the cheap ones, or a bad
  rollout becomes blanket blindness while still looking contained;
- **hard** — the rule is not retried until a *different* rule arrives, so a flaky
  link re-pushing the same ruleset cannot spend the allowance again and again;
- **reported** — the device reports the rule as `throttled`, which is one of the
  gate's three signals.

The two ceilings are the same figure, so a rule the pack allows can never trip
the one on the endpoint, and the two gates cannot disagree about a legitimate
rule.

**`throttled` is a coverage state of its own, and it is liveness.** It is not
`unsupported`: one says a rule was written wrong, the other says a host is short
of a reading, and filing the first as the second would put a rule somebody
mistuned on a customer's remediation list as a permanent platform gap. It lives
in memory with `active` and `unknown`, because it stops being true the moment the
machine reconnects and re-arms.

## Consequences

A bad curated rule costs a handful of endpoints for an hour instead of an estate,
and reverts itself without anyone being paged to do it.

Stopping a rule mid-incident is one row and takes effect on the next reconnect,
so the response to "this rule is hurting machines at 3 a.m." is not a release.

A rule that is too expensive for a machine is stopped by the machine, and the
fact reaches the server as a coverage state rather than as a mystery — the rule
stops being evaluated there, and the estate says so.

Coverage now has four states rather than three. They still add up to the fleet,
and the split by *nature* is unchanged: only "cannot evaluate" is durable.

The stage machine is decided but not yet driven: the per-organization ceiling
signal it gates on does not exist until the alert store lands, and a gate reading
a made-up signal is exactly what this decision refuses.

## Alternatives considered

**Advancing on the hold alone.** Gives a bad rule an hour's patience and then the
whole fleet. The hold is what makes the evidence meaningful, not what makes the
decision.

**Storing the chosen canary devices.** A roster has to be maintained, migrated
and reconciled, goes stale as machines are enrolled and retired, and gives a
different answer on a server that has not read it yet.

**Reverting to zero on a tripped gate.** Pulls the rule off the machines that are
proving it, and turns a signal that flaps into a rule that is alternately watching
and not watching an estate. A halt at the smallest population keeps the evidence
coming and leaves the stop to a person.

**Notifying on a revert.** A rollout that pages someone for every stage change
trains them to ignore it. Aggregate counters are how a bad rollout becomes
visible.

**A budget relative to what the rule declared.** Unfalsifiable: the evaluator
bounds its own retention from the same declaration, so a rule can never exceed
its declared cost, and the check would never fire. The ceiling has to be
absolute to catch the rule that should not have reached the endpoint at all.

**Reporting a throttle as `unsupported`.** Free — the state and its durable
storage already exist — but it conflates a mistuned rule with a platform gap, and
the gate would revert a healthy rollout every time a canary machine happened to
lack a reading.
