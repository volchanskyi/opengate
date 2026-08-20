# Investigations

Three hundred and twelve alerts across forty machines are not something a person
on call reads. One room saying "forty machines, since 02:41" is. Investigations
is where an alert lands, where a technician picks it up, and where the answer is
recorded when it is over.

What produces the alerts is in [Alerts and Rules](./Alerts-and-Rules.md); what an
administrator can retune about a rule that keeps firing is in
[Rule Administration](./Rule-Administration.md).

## Incidents: the room an alert lands in

Every stored alert is folded into an **incident** in the same write that stores it
([`internal/alerts`](../../server/internal/alerts)): an alert filed outside the
room it belongs to is invisible to the only surface anyone looks at, which is a
worse failure than the alert never arriving, because nothing says it is missing.

An alert joins an open room when all of these hold — same customer, same
`rule_id`, a grouping key the rule's scope resolves to the same value, and an
event time inside the room's window. Two axes decide it, and both carry weight:

| Rule shape | Room is about | Window | What it does |
|---|---|---|---|
| Fleet event | the customer, or one office | 30 min | A bad driver reaching forty machines is one room, not 312 rows |
| Slow burn | the machine | 24 h | A disk filling over a week re-fires into one room |
| Recurrence | the machine | 7 d | Thirty daily freezes are one room saying "thirty occurrences" |

The last row is the one the second axis exists for. No single freeze on a
workstation is worth a callout and each looks like a one-off; the pattern *is*
the diagnosis, and only grouping across time makes it visible. The same thirty
alerts under a half-hour window are thirty rooms and the finding is gone.

**The customer is the widest a room may be.** Grouping never crosses a customer
boundary: at the tenant, Contoso's driver rollout and Fabrikam's unrelated outage
would land in one room with no correct assignee. A rule's `group_by` may also
name the mount or the metric, which say which volume or dimension a firing was
about — a property of the alert rather than of the room, so a server with two
full volumes has two alerts and one room to visit it in.

**Keyed on the rule, never the rule version**, so retuning a rule while somebody
is working an incident does not fork the room out from under them. Two different
rules firing on one underlying condition stay two rooms, because they are two
findings with two remedies.

**The window is measured against the room's own span, not the clock.** That is
what makes a retroactive scan one room: thirty findings from a month of local
history all arrive in the same second, and judged against now, twenty-nine of
them would look stale and open a room each. It is two-sided, because a scan
walking history backwards produces its findings newest-first. A finding older
than the room's whole span is kept and filed under nothing — it is not part of
the story a live room is telling, and closing live work on the strength of a
three-month-old finding would be worse.

**One open room per key, enforced by the database.** Contoso's forty machines
report on forty connections at once, and the partial unique index is what makes
two of them converge on one room rather than splitting one event in two.
`occurrences` counts alerts and `device_count` counts machines — 312 and 40 are
the same event and two very different numbers — and both are restated from the
room's own alerts on every fold, so a concurrent write and an erased machine
arrive at the right number by the same route.

**A low-severity observation opens nothing on its own.** A fleet event where no
host individually breaches is visible precisely because several hosts see it at
once, so an observation is stored holding no room until a second machine reports
the same thing inside the window — and then the readings that were waiting join
the room they turned out to belong to. An observation arriving into a room that
is already open simply joins it: that is the context an investigation wants.

**Lifecycle: `new → acknowledged → investigating → resolved`.** A room in `new`
*is* the triage queue, which is why there is no separate promotion step. Every
move writes a line to the room's history, which is what a handover between two
technicians reads. Resolving requires a **cause code** from a closed set —
`resolved_self`, `fixed_by_tech`, `hardware_fault`, `expected_load`,
`false_positive`, `duplicate`, `wont_fix`. `false_positive` is the load-bearing
one: it is the only channel that says which curated rule needs its threshold
moved. Reopening is a door of its own rather than a transition, because it
withdraws an answer that has already been given; it yields when the same
condition has since recurred and opened a fresh room.

**A quiet room resolves itself after its rule's grouping window**, and that is
the same number deliberately — a room must stay open for exactly as long as a new
alert could still fold into it. Any other figure makes auto-resolve and grouping
disagree, and a recurrence fragments into a queue of one-offs. A sweep closes
them, and an alert arriving after the window closes the lapsed room on its way
past, so promptness depends on the sweep but correctness does not. A room is
stamped closed at the instant it *became* closeable rather than when it was
noticed, and carries no cause code: that is a person's answer, and the system
does not put words in their mouth.

**A machine in maintenance keeps its room.** Maintenance stops the agent
sampling, so the silence that follows is the silence the operator asked for, and
reading it as recovery would close the very incident the host work is happening
because of. The shield is only for a room about that one machine — a customer or
office room is still being reported into by the rest of the estate. See
[ADR-075](../adr/ADR-075-incident-grouping-lifecycle-and-auto-resolve.md).

## The triage workspace

The ranking that arrives inside an alert is read in the investigations workspace
([`features/investigations/`](../../web/src/features/investigations)): the triage
queue at `/investigations` and one incident's room at `/investigations/:id`,
where an alert's frozen evidence — ranked dimensions, series, processes and
redacted log lines — is rendered from the snapshot the device wrote. The room
issues no request outside `/api/v1/investigations`, so nothing on it is fetched
from the machine ([ADR-078](../adr/ADR-078-the-triage-workspace-reads-a-snapshot.md)).
A machine's own page carries a strip of the incidents it is caught up in.

| Screen | Path | What it shows |
|---|---|---|
| **Investigations** | `/investigations` | The triage queue — open incidents with severity, status, rule, how many alerts across how many machines, filtered by status, severity, rule and device, paged by cursor. Carries the per-rule coverage split |
| **Investigation Room** | `/investigations/:id` | One incident: its history, the alerts it folded, and each alert's frozen evidence; status moves, assignment and notes |

**The investigation room reads a snapshot, never a machine.** Everything it shows
comes from the incident read: the alerts, and the evidence each one carried when
it was written. Nothing on the page can be fetched from the device afterwards, so
the room states an absence — no evidence recorded, a size cap that cost the blob
something, no alerts left in the room — rather than leaving a gap a reader would
take for a pending load. The lifecycle
([`incident-lifecycle.ts`](../../web/src/features/investigations/incident-lifecycle.ts))
mirrors the moves the server permits, so a transition the server would refuse is
never offered, and resolving asks for its cause code before it is sent.

The incident read, its filters and its cursor paging are in
[API Reference](../architecture/API-Reference.md#investigations); the tables and
their closed value sets are in
[Database](../architecture/Database.md#investigation-tables).
