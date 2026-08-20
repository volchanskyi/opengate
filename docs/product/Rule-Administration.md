# Rule Administration

The operator surface for curated detection: what each rule watches, what a
customer has retuned, how far it has reached, and the switch that stops it.

What a rule *is* — its grammar, the shipped catalogue, how a binding resolves,
what each coverage state means, how a rollout advances and what an alert carries —
is in [Alerts and Rules](./Alerts-and-Rules.md). This page is the surface over
that model.

Rules are a top-level section. Every member of a tenant reads it — a technician
resolving an incident as a false alarm has to be able to see the rule that
produced it — and only an administrator changes anything. The decision behind
that split, and behind everything else on this page, is
[ADR-079](../adr/ADR-079-rule-administration-and-the-cross-cutting-label.md).

---

## What cannot be changed here

A rule's logic — what it watches, how the number it compares is derived, what its
alerts are grouped by — is versioned YAML compiled into the server and
cost-bounded in CI before it can reach a machine
([ADR-071](../adr/ADR-071-rule-catalogue-bindings-and-durable-coverage.md)). There
is no authoring surface, and the screen renders that half as description rather
than as a disabled form.

What an operator changes is the numbers each rule declares adjustable, which
machines they apply to, how far the rule has reached, and whether it runs at all.

---

## The list

One row per rule in the pack this server runs: what it watches, how far it has
reached, how many machines are running it against the fleet the count was taken
over, and a badge for how much it has raised in the last hour.

Anything wanting attention floats to the top — a rule somebody stopped, one
raising far more than it usually does, one with machines that cannot run it at
all. The ordering lives in
[`rule-summary.ts`](../../web/src/features/rules/rule-summary.ts).

### The noise badge

The count is the rule's alerts for the customer selected in the picker, over the
last hour. The colour is that count against the rule's **own** usual rate rather
than against a threshold shared across the pack, so a rule meant to be chatty
does not sit permanently red and a rule with no history yet reads neutral. The
levels and where they change are in
[`noise.go`](../../server/internal/alerts/noise.go).

---

## The rule page

### What it does

Read-only description: what it watches, what counts as bad, how long a breach
must persist, how its firings group, and what a machine must be able to read for
the rule to be evaluable on it.

### Tuning

The adjustable numbers, laid out narrowest rung first — machine, then office,
then customer — which is the order resolution reads them in. Each value shows the
range the rule allows and what the rule ships with; a value outside that range is
refused on write, where somebody can still see why.

Two values aimed at one rung by different labels are settled by an explicit
precedence, shown in the list. Across rungs there is no ambiguity: the narrower
level always wins.

**Why is this machine at 95?** Name a machine and the page resolves the rule the
way the delivery path does, showing each number in force and what decided it.

**When a rule version narrows a range**, the customer's value moves to the
nearest one the new version allows. The rule keeps firing at the moved value —
going quiet is the failure this prevents — and the move stays on the page until
an administrator acknowledges it.

### Coverage

Machines running the rule, machines that stopped running it because it cost more
than its allowance, machines that cannot run it at all, and machines that have
reported nothing — the four states defined in
[Alerts and Rules](./Alerts-and-Rules.md#threshold-alerts). They always add up to
the fleet; a split that does not is itself the finding and is said so on the
page.

### Rollout

How far the rule has reached, the population each stage reaches, how long each
stage is held before it may advance, and the stop switch.

The stop is visually and functionally separate from the on/off toggle: switching
a rule off is an ordinary choice about what a customer wants watched, stopping it
is an intervention, and afterwards the two have to be tellable apart. It works at
two scopes — one customer, or every customer in the tenant at once — and takes
effect on machines that are offline when it is used, which are stopped when they
reconnect.

A rule that misbehaves is pulled back to a smaller population by the rollout
machinery itself
([ADR-073](../adr/ADR-073-staged-rule-rollout-and-the-endpoint-budget.md)). That is
not configuration: there is no field, column or endpoint for switching it off,
and the tests assert the absence.

---

## Labels

Flat key-and-value labels a rule can be aimed at — `role=file-server`,
`env=production`. They cut across the tenancy ladder rather than sitting on a
rung of it, which is the point: the set a threshold is usually meant for spans
offices, and no rung names it.

The values come from a list each customer maintains rather than being typed in,
and machines are labelled in bulk from
[the labels page](../../web/src/features/rules/DeviceLabels.tsx). A machine carries
at most one value per key.

**Deleting a label a rule aims at is refused.** Removing it would take a tuned
value off every machine that carried it, which does not read as a deletion — it
reads as a threshold that quietly widened across an estate one afternoon.

---

## Alert limits

A customer's alert budget, on its own page rather than on a rule: it is the
safety net under all of them rather than a property of any one.

Two numbers, both per customer. How many alerts the customer may store in a
rolling hour across every machine, and how many one of their machines may raise
in a rolling hour. Each may be raised only as far as a maximum that lives in code
— see the constants in [`limits.go`](../../server/internal/alerts/limits.go) —
because a limit an operator can raise without bound is not a limit.

The per-machine half is enforced **on the machine**. It travels down with the
ruleset on the same message and is applied to the running alert sink, so a change
lands on the next alert rather than at the next restart. A screen that only wrote
a database row would have changed nothing: a server-side check would receive the
flood it exists to prevent.

Nothing a ceiling refuses is lost quietly. What it turns away is counted and
folds into one incident that says how much
([ADR-074](../adr/ADR-074-alert-store-accounted-ingest-and-the-erasure-cascade.md)).

---

## Who may do what

| Action | Access |
|---|---|
| Read the pack, a rule, its tuning, its coverage, the labels, the budget | Any member of the tenant |
| Retune a value, aim it, set its precedence | Administrator |
| Enable, disable, stop or resume a rule | Administrator |
| Change rollout populations and waiting periods | Administrator |
| Change the alert budget | Administrator |
| Manage labels and assignments | Administrator |
| Acknowledge a value a rule version moved | Administrator |

Every write lands in the audit log with the actor and the moment. The permission
and audit contracts are asserted endpoint by endpoint in
[`handlers_rules_access_test.go`](../../server/internal/api/handlers_rules_access_test.go),
so an endpoint added later without either fails a test rather than being
discovered from an incident nobody can attribute.

The administrator flag is per tenant, not per customer — there is no
administrator scoped to one customer — so tuning is a platform-operator job.

---

## Where it lives

| Layer | Path |
|---|---|
| Screens | [`web/src/features/rules/`](../../web/src/features/rules) |
| API handlers | [`handlers_rules_read.go`](../../server/internal/api/handlers_rules_read.go), [`handlers_rules_tuning.go`](../../server/internal/api/handlers_rules_tuning.go), [`handlers_rules_rollout.go`](../../server/internal/api/handlers_rules_rollout.go), [`handlers_device_tags.go`](../../server/internal/api/handlers_device_tags.go), [`handlers_alert_limits.go`](../../server/internal/api/handlers_alert_limits.go) |
| Tuning, labels, rollout, clamps | [`server/internal/rules/`](../../server/internal/rules) |
| Alert budget and the noise count | [`server/internal/alerts/`](../../server/internal/alerts) |
| Storage | [`016_rule_administration.up.sql`](../../server/internal/db/migrations/016_rule_administration.up.sql) |
| The endpoints themselves | [`openapi.yaml`](../../api/openapi.yaml) |
