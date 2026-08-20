# Investigations

Three hundred and twelve alerts across forty machines are not something a person
on call reads. One incident saying *forty machines, since 02:41* is.

Investigations is where an alert lands, where a technician picks it up, and where
the answer is recorded when it is over.

## Screens

| Screen | Path | What it is for |
|---|---|---|
| Triage queue | `/investigations` | Open incidents, filtered and paged — the working list |
| Incident room | `/investigations/:id` | One incident: its history, the alerts it folded, and each alert's evidence |
| Device incident strip | On the device page | The open incidents that machine is caught up in |

The queue shows severity, status, the rule that produced the incident, how many
alerts across how many machines, and the rule's coverage split. It filters by
status, severity, rule and device.

## How alerts group into incidents

Every stored alert is folded into an **incident** in the same write that stores
it. An alert filed outside the incident it belongs to would be invisible to the
only surface anyone looks at — a worse failure than the alert never arriving,
because nothing says it is missing.

An alert joins an open incident when **all** of these hold:

- same customer;
- same rule;
- the rule's grouping key resolves to the same value;
- the event time falls inside the incident's window.

Two axes decide what an incident is about and how wide its window is:

| Rule shape | Incident is about | Window | What that buys |
|---|---|---|---|
| Fleet event | The customer, or one site | 30 minutes | A bad driver reaching forty machines is one incident, not 312 rows |
| Slow burn | The machine | 24 hours | A disk filling over a week re-fires into one incident |
| Recurrence | The machine | 7 days | Thirty daily freezes are one incident saying "thirty occurrences" |

The last row is what the second axis exists for. No single freeze on a
workstation is worth a callout, and each looks like a one-off; the **pattern** is
the diagnosis, and only grouping across time makes it visible. The same thirty
alerts under a half-hour window would be thirty incidents, and the finding would
be gone.

### Rules the grouping always obeys

- **The customer is the widest an incident may be.** Grouping never crosses a
  customer boundary — one incident holding Contoso's driver rollout and
  Fabrikam's unrelated outage would have no correct assignee.
- **A rule may also group by mount or metric**, which says which volume or
  reading a firing was about. That is a property of the alert, not of the
  incident: a server with two full volumes produces two alerts and one incident to
  visit them in.
- **Keyed on the rule, never the rule version**, so retuning a rule while somebody
  is working an incident does not fork the room out from under them.
- **Two different rules firing on one underlying condition stay two incidents**,
  because they are two findings with two remedies.
- **The window is measured against the incident's own span, not against the
  clock.** That is what makes a retroactive scan one incident: thirty findings
  from a month of local history all arrive within the same second, and judged
  against "now" twenty-nine of them would look stale and open an incident each. A
  finding older than the incident's whole span is kept and filed under nothing —
  it is not part of the story a live incident is telling.
- **One open incident per key, enforced by the database.** Forty machines
  reporting on forty connections at once converge on one incident rather than
  splitting one event in two.
- **`occurrences` counts alerts and `device_count` counts machines.** 312 and 40
  are the same event and two very different numbers. Both are restated from the
  incident's own alerts on every fold, so a concurrent write and an erased machine
  arrive at the right number by the same route.
- **A low-severity observation opens nothing on its own.** A fleet event where no
  single host individually breaches is visible precisely because several hosts see
  it at once, so an observation waits until a second machine reports the same
  thing inside the window — then the readings that were waiting join the incident
  they turned out to belong to. An observation arriving into an incident that is
  already open simply joins it.

## Working an incident

### Statuses

`new → acknowledged → investigating → resolved`

An incident in `new` **is** the triage queue, which is why there is no separate
promotion step. Every move writes a line to the incident's history — that history
is what a handover between two technicians reads.

### Resolving

Resolving requires a **cause code** from a closed set:

| Cause code | Means |
|---|---|
| `resolved_self` | The condition cleared on its own |
| `fixed_by_tech` | Someone fixed it |
| `hardware_fault` | Faulty hardware |
| `expected_load` | Real, and expected — a backup, a batch job |
| `false_positive` | The rule was wrong |
| `duplicate` | Another incident covers it |
| `wont_fix` | Understood and accepted |

`false_positive` is the load-bearing one: it is the only channel that says which
curated rule needs its threshold moved. Use it accurately.

### Reopening

Reopening is a door of its own rather than an ordinary status move, because it
withdraws an answer that has already been given. It yields when the same
condition has since recurred and opened a fresh incident.

### Auto-resolve

A quiet incident resolves itself after its own rule's grouping window — the same
number, deliberately. An incident must stay open for exactly as long as a new
alert could still fold into it; any other figure makes auto-resolve and grouping
disagree, and a recurrence fragments into a queue of one-offs.

- A sweep closes lapsed incidents, and an alert arriving after the window closes
  the lapsed incident on its way past. Promptness depends on the sweep;
  correctness does not.
- An incident is stamped closed at the instant it **became** closeable, not when
  it was noticed.
- An auto-resolved incident carries **no cause code**. That is a person's answer,
  and the system does not put words in their mouth.

### Machines in maintenance

A machine in maintenance keeps its incidents open. Maintenance stops the agent
sampling, so the silence that follows is the silence the operator asked for, and
reading it as recovery would close the very incident the host work is happening
because of.

The shield applies only to an incident about that one machine. A customer-wide or
site-wide incident is still being reported into by the rest of the estate.

## The incident room

The room shows the incident's history, the alerts it folded, and each alert's
frozen evidence: the ranked dimensions the machine produced, a stretch of those
readings, what was running, and redacted log lines.

**The room reads a snapshot, never a machine.** Everything on the page comes from
the incident record. Nothing can be fetched from the device afterwards — so the
room *states* an absence rather than leaving a gap a reader would mistake for a
pending load:

- no evidence recorded;
- a size cap that cost the evidence something;
- no alerts left in the incident.

Status moves offered in the room mirror exactly what the server permits, so a
transition that would be refused is never offered, and resolving asks for its
cause code before it is sent.

## Related

- [Alerts and Rules](./Alerts-and-Rules.md) — what produces the alerts
- [Rule Administration](./Rule-Administration.md) — retuning a rule that keeps firing
- [Device Health](./Device-Health.md) — the readings behind the evidence
