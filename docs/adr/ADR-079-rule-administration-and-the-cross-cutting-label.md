---
adr: 079
title: "Rule Administration, and the Cross-Cutting Label"
status: Accepted
date: 2026-08-18
---

# ADR-079: Rule Administration, and the Cross-Cutting Label

## Status

Accepted.

## Context

[ADR-071](ADR-071-rule-catalogue-bindings-and-durable-coverage.md) built a full
rule configuration system and
[ADR-073](ADR-073-staged-rule-rollout-and-the-endpoint-budget.md) built the
machinery that spreads a rule across an estate. The only operator either of them
had was a database console.

Three things had no path to a person at all.

**A rule tuned wrong stayed wrong.** The `false_positive` resolution code
([ADR-075](ADR-075-incident-grouping-lifecycle-and-auto-resolve.md)) exists to
say which rule needs moving. Nothing could act on that answer.

**The stop switch was unreachable.** It is the mitigation for the one High-impact
risk in the programme — a bad rule degrading five thousand machines — and it was
a boolean column nobody could flip.

**The alert ceilings were guesses.** Both were chosen as a multiple of a rate
that had never been measured on a live estate. A wrong guess that needs a release
to correct is an outage.

Four decisions shaped the answer, and each has an alternative that looks
reasonable and is wrong.

**Who may change what.** The database enforces isolation at the tenant; an
organization is a scoping level inside it, and `users.is_admin` means "may change
configuration anywhere in this tenant". There is no administrator scoped to one
customer, so tuning is a platform-operator job and a technician reads. Inventing
a customer-scoped administrator here would put a second, weaker notion of
"admin" beside the one the tenancy model has — that belongs in the tenancy work.

**How a rule is aimed at a set of machines.** The set a threshold is usually
meant for — the file servers — spans offices and does not correspond to any rung
of the tenancy ladder. Adding a rung for it would make every machine's place in
the fleet depend on what somebody once wanted to tune.

**What happens when a rule version narrows what it accepts.** A rule upgrade
applies by itself and keeps the customer's tuning, so a narrowed range inherits
values outside it. Dropping them reverts an estate to a default nobody asked
for, silently; keeping them puts a value on the wire the rule's author refused.

**Whether the automatic pull-back is configurable.** Every other rollout setting
is the customer's. Making this one theirs too would be consistent, and would hand
away the only thing standing between a bad rule and an estate.

## Decision

**A top-level Rules section, read by everyone in the tenant and written only by
an administrator.** A technician resolving an incident as a false alarm has to be
able to see the rule that produced it. Every write is administrator-gated and
lands in the audit log; the permission and audit contracts are asserted endpoint
by endpoint rather than in aggregate, so a route added later without either fails
a test.

**A rule's logic is rendered as description and never as a form.** Definitions are
compiled in and cost-bounded in CI; there is no authoring surface and the screen
does not imply one. What an operator changes is the numbers the rule declares
adjustable, who it reaches, and whether it runs.

**Labels are a cross-cutting dimension, chosen from a list each customer
maintains.** A flat key and value — `role=file-server` — carried by a machine and
matched by a binding's selector. The values come from a stored list rather than
free text, because a targeting dimension where `production`, `Production` and
`prod` are three estates reaches a third of the machines it was meant for.
Deleting a label a rule aims at is refused: removing it would take a tuned value
off every machine that carried it, which reads as a threshold quietly widening
rather than as a deletion.

**Two labels matching one machine at one rung are settled by a precedence the
operator sets and can see.** Across rungs there is no ambiguity — the narrower
level always wins — so precedence only settles ties inside a level. Every
invisible tie-break (newest wins, alphabetical, row order) produces a threshold
nobody can predict from the screen.

**A value a new rule version no longer allows moves to the nearest one it does,
and the move is flagged until an administrator acknowledges it.** The rule keeps
firing at the moved value throughout: going quiet is the failure this exists to
prevent. The move is recorded once per binding, parameter and version, so the
same upgrade read a hundred times is one flag.

**Both alert ceilings are editable per customer, each with a maximum that lives
in code rather than beside the value.** A limit an operator can raise without
bound is not a limit. The per-machine ceiling is enforced where alerts are raised,
so it travels down with the ruleset on the same message and is applied to the
running sink rather than at the next restart — a check at the server would
receive the flood it exists to prevent.

**Rollout populations and waiting periods are per rule and per customer. The
automatic pull-back is not settable through any route.** It carries no field, no
column and no endpoint, and the absence is asserted rather than assumed.

**A stop exists at two scopes — one customer, and every customer in the tenant at
once — and is deliberately separate from switching a rule off.** Switching off is
an ordinary choice; a stop is an intervention, and afterwards the two have to be
tellable apart. Both take effect through the reconnect path, so a machine that
was offline when somebody reached for the switch is stopped when it returns.

**The noise badge counts a rule's recent alerts for the selected customer and
colours them against that rule's own usual rate.** A rule meant to be chatty does
not sit permanently red, and a rule with no history yet reads neutral rather than
alarming. The count is scoped to the customer in the query itself: row-level
security stops it crossing a tenant, and nothing at all stops it crossing a
customer inside one.

## Consequences

A curated rule can now be retuned, aimed, paced and stopped by a person, and
every one of those actions is attributable. The `false_positive` resolution code
has somewhere to lead.

Labels are a new concept in the product with a maintenance cost of their own: a
list per customer, an assignment per machine, and a deletion that can be refused.
That cost buys the only targeting dimension that matches how thresholds are
actually reasoned about.

The per-machine ceiling now rides `PushAlertRules`, so the wire contract and both
golden fixtures carry it. An agent that ignored the field would keep its own
allowance rather than the customer's, which the reverse-golden assertion is
written to catch.

A customer-scoped administrator remains out of scope, and with it any prospect of
a customer tuning their own thresholds. Revisit in the tenancy work when a
customer needs to self-serve.

## References

- [ADR-071](ADR-071-rule-catalogue-bindings-and-durable-coverage.md) — the
  catalogue, bindings and durable coverage this administers.
- [ADR-073](ADR-073-staged-rule-rollout-and-the-endpoint-budget.md) — the staged
  rollout whose populations and holds become settings here.
- [ADR-074](ADR-074-alert-store-accounted-ingest-and-the-erasure-cascade.md) —
  the accounted ingest whose ceiling becomes a per-customer budget.
- [Rule Administration](../Rule-Administration.md) — how the screen is used.
