---
adr: 071
title: "Embedded Rule Catalogue, Postgres Bindings, and Durable Unsupported Coverage"
status: Accepted
date: 2026-08-13
---

# ADR-071: Embedded Rule Catalogue, Postgres Bindings, and Durable Unsupported Coverage

## Status

Accepted.

## Context

The grammar in [ADR-070](ADR-070-rule-grammar-and-coverage.md) says what a rule
may express. It does not say where a rule lives, who may change what about it, or
how a customer stops one.

Those three questions have different answers, and answering them together is what
goes wrong. A threshold is exactly the thing an operator retunes: Contoso runs
`disk-critical` at 90, its file servers fill by design and want 95, and DAL-WS-012
is a workstation where 90 is right. That is configuration. A rule's *predicate*,
on the other hand, is the thing that decides what an endpoint computes on every
sample, for every machine in every estate — and that is not configuration, it is
code with a cost.

The cost is the point. `rule_cost` answers, from a rule's declared fields alone,
how many readings that rule asks an endpoint to hold. Computed at build time
against a budget, it is a gate: a rule that would ask five thousand endpoints to
retain an hour of samples fails on a machine that costs nothing and inconveniences
one engineer. The same check in a runtime API path is a production incident with a
validator bug in the middle of it.

Coverage raises a separate question. A rule that quietly evaluates on half an
estate while reading as healthy is the failure coverage exists to prevent, and
ADR-070 left where it is stored open. Contoso's forty containerized agents can
never evaluate `io-stalled` — the kernel's pressure accounting is not
per-container — which is a standing 8 % hole in Contoso's monitoring. Today the
answer to "how much of Contoso is blind to `io-stalled`?" depends on when the
server last restarted, which makes a monthly coverage report non-reproducible.

## Decision

**A rule has three layers, separated by how mutable each one is.**

**Definitions are versioned YAML compiled into the server.** Predicate, grammar,
evidence spec, `group_by`, `group_window` and shipped numbers live in
[`internal/rules/catalogue/`](../../server/internal/rules/catalogue/) and are
`go:embed`-ed. A YAML typo is a build failure, not a runtime 500, and there is
deliberately no load-from-disk fallback — that is the runtime-mutable path this
decision rejects. Loading refuses an unknown field, a missing or out-of-vocabulary
`group_by`, a metric outside what the fleet collects, a comparator or predicate
outside the grammar, a duplicate `(rule_id, version)`, and a rule shipping a
default its own declared bounds would refuse.

**Definitions are immutable per `(rule_id, version)`,** enforced against digests
committed in `catalogue.lock`. An alert raised last week has to still mean what it
meant then; editing a published definition is refused at load, and changing a rule
means a new version and a new line. The digest covers what a rule *means*, not how
it is described, so improving a summary is not a version bump.

**The cost gate runs at build time, per rule and across the pack.** A per-rule
ceiling alone does not bound an endpoint — enough rules just inside it would still
sink one — so the catalogue's total is capped too. Both fire in CI.

**Bindings live in Postgres, keyed down the tenancy ladder.** They carry only
values the rule declares tunable, validated against that rule's own bounds **on
write**, so a number nobody would want is refused while an operator is still
looking at it. Resolution is device → site → organization → tenant → shipped, and
the ordering is not defined here: it is
[`internal/settings`](../../server/internal/settings/settings.go), shipped by
ADR-064. **Each parameter resolves on its own**, so a customer-wide sustain window
survives one machine's retuned threshold — resolving a binding as a unit would
silently drop the sustain, which is the loss nobody notices until an alert does
not arrive.

The file-server-at-95 case therefore needs no special machinery: put the file
servers in a site. A binding may additionally carry a **bounded tag selector**
with an operator-set `precedence` breaking ties between two selectors at one rung.
Across rungs the narrower always wins; a targeted binding beats the rung's blanket
one. Two selectors at one rung sharing a precedence **cannot be stored** — a
partial unique index refuses the pair — so resolution never depends on row order,
and the resolver still orders deterministically as a last resort.

**Rollout state lives in Postgres,** because stopping a rule cannot require a
deploy. A customer with **no row has not configured the rule** and gets it as
shipped; absence is never read as "switched off", or a fresh customer would be
silently unmonitored while looking healthy. `kill` is deliberately separate from
`enabled`: switching a rule off is an ordinary choice, a kill is an intervention,
and the two must be distinguishable afterwards. A kill is filed on the customer,
which is the whole point of where it lives — no narrower rung carries one, so
nothing set on one machine can undo it.

**A store that cannot be read is reported, not papered over.** Substituting the
shipped defaults would push rules ignoring whatever the customer set, including a
kill switch, at exactly the moment somebody reached for it. The agent keeps the
ruleset it already holds, so the cost is a ruleset that is not refreshed rather
than a machine that stops being watched.

**Coverage is stored by the nature of each state, not together.** `active` and
`unknown` are liveness and stay in memory: they are *supposed* to reset when the
server loses sight of the fleet. FS01, decommissioned three weeks ago without
anyone saying so, is `unknown` — which is exactly true, where a stored `active`
would claim 500/500 machines watched when the real number is 499.

`unsupported` is durable and is persisted, under two rules that are the FS01 case
in disguise. **Only `unsupported` is ever written:** a device reporting `active`
for a rule it has a row for **deletes** that row rather than flipping it, so there
is no stored `active` that can go stale, and steady state costs **zero writes** —
a write happens on a state change, never on a summary. **Deleting a device erases
its coverage rows**, by cascade, or a decommissioned container inflates the
`unsupported` count forever. The read becomes: `unsupported` = persisted rows for
devices still in the fleet, `active` = what memory holds, `unknown` = fleet −
active − unsupported. The identity still holds.

All three tables carry forced RLS on `tenant_id` through one shared
`app_tenant_visible` predicate — stated once so an edit cannot reach two tables
and miss the third — plus a composite `(tenant_id, organization_id)` foreign key,
so a row naming a customer from another tenant is refused by the database rather
than by a check somebody has to remember to run.

## Consequences

Adding or retuning a rule definition is a deploy, which is the intended trade: it
buys the build-time cost gate and version immutability. Retuning a *number* is
not a deploy, which is what customers actually ask for.

Tag selectors are stored and resolved but match nothing until devices carry tags;
bindings filed against a rung work today, which is the path the file-server case
uses. The selector mechanism starts working the moment tags exist, with no schema
change.

Coverage reads now touch Postgres. The write path does not, in steady state.

## Alternatives considered

**Definitions in Postgres.** Moves the program's highest-impact gate out of CI:
cost-bounding a predicate before it reaches 5 000 endpoints is mandatory and free
at build time, and a validator bug in a runtime API path is a production incident.

**Definitions with no database layer at all.** Cannot express a kill switch, a
canary state, or coverage — the wall Netdata hit before adding DYNCFG.

**Persisting all three coverage states with `last_reported_at` plus a staleness
window on read.** Buys nothing the split does not, pays a write per summary per
device, and reintroduces the FS01 lie in a form that now needs a staleness rule to
suppress it.

**A rule-authoring UI.** Excluded by the program's scope: the curated pack is the
product, and an authoring surface is the unbounded-grammar path ADR-070 refuses.
