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
   ship. Every definition states how bad its alerts are, and a rule that reads
   the machine's own log records states no reading, no comparison and no line —
   it is registered rather than evaluated centrally, and costs the machine
   nothing per rule because one bounded poll a minute covers the whole pack.
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

**A rule reaches a customer's machines in stages, and an administrator moves each one.**
The rollout request names the stage — the first share, the second share or
everyone — and the reach follows the share it names
([`handlers_rules_rollout.go`](../../server/internal/api/handlers_rules_rollout.go));
nothing moves a stage on its own. NinjaOne's patch rings are approved by hand,
LaunchDarkly offers a share set by hand beside timed and watched ones, and
Microsoft Autopatch moves its rings on a schedule. Membership in a stage is
computed per machine, not stored. The first stage has a floor of five machines,
bounded by the fleet.

**Stopping a rule is a row, not a release, and it does not wait.** A kill on the
rollout row stops it everywhere it applies. A machine's link is held open for as
long as it is healthy, so a change delivered only on registration would be
delivered only when something unrelated broke — the change goes out to that
customer's connected machines as it is made, and to an offline machine as it
arrives. Retuning travels the same path, because a boundary nobody's machines
are comparing against is a row in a table, and so do removing a retuned number
and a change to the customer's per-machine alert allowance, which rides down with
the rules.

**Each customer's rule settings are held in memory.** Its retuned numbers,
rollouts and per-machine alert allowance are read for every customer before the
server takes machines, each inside that customer's own tenant so the tenant wall
still guards the read, and a server whose read fails does not start
([`alert_rules_memory.go`](../../server/internal/agentapi/alert_rules_memory.go)).
A sign-in then reads only the machine's own rows. Every change made through the
product reads its customer again before it goes out, and a change made outside
the product is picked up by the next start. Each read carries a stamp, and a
sign-in whose ruleset was built before a change landed pushes again, up to three
times. The memory belongs to one server copy, and production replaces its server
rather than running two side by side
([`server-deployment.yaml`](../../deploy/helm/opengate/templates/server-deployment.yaml)).
MeshCentral serves device groups from memory the same way; a reload on a timer,
as Zabbix's configuration cache does, was not taken, since a change would wait up
to one period.

**A rule's revision and its severity travel with it.** An alert is identified by
the machine, the rule, that revision and the window it fired for, so a machine
that was never told which revision it is running could raise nothing the server
accepts. Severity orders the queue, and an alert that states none cannot be
ordered in it.

**The endpoint enforces its own budget**, over what a rule actually cost rather
than what it was projected to cost — per rule, so one expensive rule cannot
silence the cheap ones; hard, so a flaky rule does not retry until a different
rule arrives; and reported, so the machine says `throttled` rather than quietly
doing less.

## Consequences

Retuning a boundary is a row change. Changing what a rule means is a new version
with its own identifier, and the retroactive scan runs again for it.
