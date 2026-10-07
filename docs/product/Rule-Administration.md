# Rule Administration

- [Screens](#screens)
- [What cannot be changed here](#what-cannot-be-changed-here)
- [The rule list](#the-rule-list)
  - [The noise badge](#the-noise-badge)
- [The rule page](#the-rule-page)
  - [Default Config](#default-config)
  - [Tuning](#tuning)
  - [Coverage](#coverage)
  - [Rollout](#rollout)
- [Labels](#labels)
- [Alert limits](#alert-limits)
- [Who may do what](#who-may-do-what)
- [Related](#related)

The operator surface for curated detection: what each rule watches, what a
customer has retuned, how far the rule has reached, and the switch that stops it.

What a rule *is* — its grammar, how tuning resolves, what each coverage state
means, how a rollout advances and what an alert carries — is in
[Alerts and Rules](./Alerts-and-Rules.md). This chapter is the screens over that
model.

## Screens

| Screen | Path | Contents |
|---|---|---|
| Rules | `/rules` | One row per rule, in two groups: what it watches, how far it has rolled out, its four coverage counts against the fleet, and how much it has raised in the last hour |
| Rule detail | `/rules/:ruleId` | Explanation, default config, tuning, coverage and rollout for one rule |
| Labels | `/rules/labels` | The label values a customer maintains, and labelling hosts picked from a checklist |
| Alert limits | `/rules/alert-limits` | The customer's alert budget |

**Rules is a top-level section.** Every member of the tenant can read it — a
technician resolving an incident as a false alarm has to be able to see the rule
that produced it. Only an administrator can change anything.

## What cannot be changed here

A rule's logic — what it watches, how the compared number is derived, what its
alerts group by — ships with the release. There is no authoring surface, and the
screen renders that half as description rather than as a disabled form.

What an operator changes is: the numbers a rule declares adjustable, which
machines they apply to, how far the rule has reached, and whether it runs at all.

A rule that reads the machine's own log records declares no numbers at all,
because there is nothing in it to compare — it matches phrases rather than
crossing a line. Its row shows what it means in words, and the only thing an
operator changes about it is whether it runs.

## The rule list

The list is in two groups, each a heading that folds its rules away and back:
**Host rules**, which compare a reading against a line, and **Linux rules**, which
match the host's own log records. Both start open. Within a group, anything
wanting attention floats to the top — a rule somebody stopped, a rule raising far
more than it usually does, a rule with hosts that cannot run it at all.

Each row counts the fleet four ways — **Watching**, **Paused: too costly**,
**Can't run here** and **Not heard from** — each as *n / fleet*, with *Can't run
here* in red above zero. The line under the title says what the counts are taken
against (*Counted against 312 hosts*), and a rule whose four counts do not add up
to the fleet is named in a warning above the list.

### The noise badge

Each row carries a count of that rule's alerts for the customer selected in the
picker, over the last hour.

The colour compares that count against **the rule's own usual rate**, not against
a single number shared across the pack. A rule that is meant to be chatty does not
sit permanently red, and a rule with no history yet reads neutral.

## The rule page

Under the rule's name, *Rule explanation:* gives the rule's own one-line summary.

### Default Config

Read-only description: what the rule watches, what counts as bad, how long a
breach must persist, how its firings group, and what a host must be able to read
for the rule to be evaluable on it. It says, beside the rule's version, that this
config ships as part of the rule and cannot be changed.

### Tuning

The adjustable numbers, laid out **narrowest rung first** — machine, then site,
then customer — which is the order resolution reads them in.

- Each value shows the range the rule allows and the value it ships with.
- A value outside the allowed range is refused on write, where somebody can still
  see why.
- Two values aimed at one rung by different labels are settled by an explicit
  precedence, shown in the list. Across rungs there is no ambiguity: **the
  narrower level always wins**.
- With nothing set, the page says *No values set (Ships with the default
  values)*.

An administrator sets a value for one **site**, picked from the chosen customer's
sites, with **Apply to site**. Each setting is offered by its plain name with the
stored name beside it — *Alert level (threshold)*, *All-clear level (clear)*,
*Must last for (sustain_secs)*, *Averaging window (window_secs)* — and as a value
is typed the page says what it will do (*An alert is raised when cpu.total reads
at or above 95.*) and marks it on a bar of the allowed range beside the shipped
value.

**"Why is this host at 95?"** Pick a host — sorted by name, each with its online
or offline dot — and the page resolves the rule exactly the way the delivery path
does, showing each number in force and what decided it. Until a host is picked it
reads *Select a host to see current values.*

**When a new rule version narrows a range**, the customer's value moves to the
nearest value the new version allows. The rule keeps firing at the moved value —
going quiet is the failure this prevents — and the move stays visible on the page
until an administrator acknowledges it.

### Coverage

The four states from [Alerts and Rules](./Alerts-and-Rules.md#threshold-alerts) —
machines running the rule, machines that stopped it for costing too much,
machines that cannot run it at all, and machines that have reported nothing.

They always add up to the fleet. A split that does not add up is itself a
finding, and the page says so rather than quietly showing wrong numbers.

### Rollout

How far the rule has reached, the population each stage covers, how long a stage
is held before it may advance, and the stop switch.

**The stop switch is visually and functionally separate from the on/off toggle.**
Switching a rule off is an ordinary choice about what a customer wants watched;
stopping it is an intervention, and afterwards the two have to be tellable apart.

| Property | Behaviour |
|---|---|
| Scope | One customer, or every customer in the tenant at once |
| Connected machines | Reached as the switch is thrown |
| Offline machines | Stopped when they reconnect |
| Precedence | A stop outranks the stage — the canary machines proving the rule lose it too |
| Deploy needed | None |

A retuned number travels the same way. A boundary sitting in a table is not one
any machine is comparing against, so a change reaches that customer's connected
machines as it is made rather than the next time each one happens to reconnect.

A rule that misbehaves is pulled back to a smaller population by the rollout
machinery itself. That is not configuration: there is no field, column or endpoint
for switching that behaviour off.

## Labels

Flat key-and-value labels a rule can be aimed at — `role=file-server`,
`env=production`.

Labels cut **across** the tenancy ladder rather than sitting on a rung of it, which
is the point: the set of machines a threshold is meant for usually spans several
sites, and no rung of the ladder names that set.

- Values come from a list each customer maintains, rather than being typed in
  free-form.
- Hosts are labelled in bulk from the labels page: pick a label, tick the hosts
  from the chosen customer's checklist, and assign.
- *Which host carries what* names each host by hostname; a host removed since
  reads as *a removed host*.
- A host carries at most one value per key.

> **Deleting a label a rule aims at is refused.** Removing it would take a tuned
> value off every machine that carried it — which does not read as a deletion, it
> reads as a threshold that quietly widened across an estate one afternoon.
> Re-aim or remove the rule's binding first.

## Alert limits

A customer's alert budget lives on its own page rather than on any one rule: it is
the safety net under all of them.

| Limit | Scope |
|---|---|
| Customer hourly ceiling | How many alerts the customer may store in a rolling hour, across every machine |
| Per-machine hourly ceiling | How many alerts one of their machines may raise in a rolling hour |

Each may be raised only as far as a maximum that lives in code, because a limit an
operator can raise without bound is not a limit.

**The per-machine half is enforced on the machine.** It travels down with the
ruleset and is applied to the running alert queue, so a change lands on the next
alert rather than at the next restart. A screen that only wrote a database row
would have changed nothing — a server-side check would still receive the flood it
exists to prevent.

Nothing a ceiling refuses is lost quietly: what it turns away is counted and folds
into one incident that says how much.

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

Every write lands in the audit log with the actor and the moment.

> The administrator flag is **per tenant, not per customer** — there is no
> administrator scoped to a single customer — so rule tuning is a platform-operator
> job, not something delegated per account.

## Related

- [Alerts and Rules](./Alerts-and-Rules.md) — the detection model itself
- [Investigations](./Investigations.md) — the queue a noisy rule shows up in
- [Tenancy and Access](./Tenancy-and-Access.md) — the ladder tuning resolves down
