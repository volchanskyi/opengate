---
number: 70
title: The alert-rule engine
---

# ADR-070 — The alert-rule engine

## Context

A rule runs on a customer's machines, on the machine itself, on a processor
budget the customer is paying for. So a rule has to be cheap to evaluate,
impossible to write as an unbounded loop, and stoppable without a release.

## Decision

**A rule is data in a closed grammar.** A selector, a comparator, a boundary,
and a window function — instantaneous value, rate, window maximum, or window
mean. Closed means the cost of any rule can be computed before it runs.

**A conjunction is one rule, not two.** Two terms that must both hold are one
rule with one clear boundary, so recovery happens when either side genuinely
recovers past its own boundary rather than when one of two separate rules
happens to clear.

**Dimension names are canonical, with aliases.** A rule written against an
older name still resolves.

**Coverage is a state per device per rule, and the three states add up.** A rule
is `active` on a machine, `unsupported` on it, or `throttled`. `unsupported` is
a first-class answer, not an error: a disk-latency rule on a machine whose
kernel cannot report it is not failing, it is inapplicable — and a technician
needs to see that rather than an empty space. Coverage rides the health summary
and has no table of its own.

**Three layers, separated by how often each changes.**

1. **Definitions are versioned YAML compiled into the server.** Immutable per
   identifier and version, checked against digests. A cost gate runs at build
   time, per rule and across the whole pack, so an unaffordable rule cannot
   ship.
2. **Bindings live in Postgres**, keyed down the tenancy ladder. A customer
   chooses which rules apply and at what boundary; it cannot change what a rule
   means.
3. **Rollout state lives in Postgres**, because stopping a rule cannot require
   a release.

**A store that cannot be read is reported, not guessed at.** Substituting
defaults for bindings nobody could load would silently apply the wrong rules to
a real fleet.

**A new rule is re-run over the machine's own stored history, once per
version.** Findings are stamped with the minute they happened, not the minute
they were found. The scan is scheduled around the machine's other work, walks
history in bounded chunks, and resumes from a durable cursor after a restart.
The device remembers which versions it has scanned, so a redeploy does not
re-scan. Findings spend the same alert allowance as anything else.

**A rule reaches a customer's machines in stages, and each stage is earned.** The stage
machine reads real signals through a port rather than inferring from elapsed
time. Membership in a stage is computed per machine, not stored. The first stage
has a floor of five machines, bounded by the fleet, and holds until it has been
quiet for long enough to mean something.

**Stopping a rule is a row, not a release.** A kill on the rollout row stops it
everywhere it applies, immediately.

**The endpoint enforces its own budget**, over what a rule actually cost rather
than what it was projected to cost — per rule, so one expensive rule cannot
silence the cheap ones; hard, so a flaky rule does not retry until a different
rule arrives; and reported, so the machine says `throttled` rather than quietly
doing less.

## Consequences

Retuning a boundary is a row change. Changing what a rule means is a new version
with its own identifier, and the retroactive scan runs again for it.
