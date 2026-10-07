---
number: 77
title: The investigations API and workspace
---

# ADR-077 — The investigations API and workspace

## Context

An incident is work, not configuration. The people who do it are ordinary
members of the tenant, and the queue they work from has to stay fast when there
are ten thousand open items in it.

## Decision

**Tenant membership is the whole gate.** Working an incident is not an
administrator action. Naming a customer narrows the view; it does not permit
anything the caller could not already see.

**The queue is keyset-paged, and the index is the budget.** Offset paging gets
slower the further in a technician goes, which is exactly where a busy queue is
worked. The performance requirement is asserted as a query plan rather than a
stopwatch, because a stopwatch on a test machine measures the test machine.

**Evidence is a separate call, decoded on the server.** It is large and most
rows are never opened.

**The device strip is the queue filtered, not a second list.** One source of
truth for what is open.

**The queue opens on New and narrows one status at a time.** A technician works
one stage of the lifecycle at a pass, and one status keeps the work to start apart
from the work already under way. The request carries the status as a one-item
list, so the API's filter stays a list.

**The server names what the screen would otherwise show as an id.** An incident
carries the name of the host, site or customer it is about, each folded alert the
hostname that raised it, and the room the display names of the people its
history names. The names are read in the same scoped transaction, each lookup
naming the tenant itself, because the user list is administrator-only and an id
is not something a person can act on. A removed record reads as removed, never as
its id.

**The rules endpoint is a coverage view, not an editor.** Changing a rule is
[ADR-079](ADR-079-rule-administration.md).

**The workspace reads the incident's snapshot and nothing else.** Evidence was
frozen on the machine at the moment it fired, and the room shows that, not a
fresh reading that would disagree with it.

**An absence is stated, never left as a gap.** A reading the machine could not
take says so. A blank space reads as zero to everyone who sees it.

**Coverage lives on the Rules list, not in the queue**
([ADR-079](ADR-079-rule-administration.md)): how much of the fleet a rule watches
is a question about the rule, asked where the rules are.

**Evidence is drawn without a charting engine.** A series here is a fixed
handful of points; pulling in the chart bundle for that would cost more than the
drawing.

## Consequences

The lifecycle — which status may follow which — is mirrored in the browser and
drives what the screen offers, so a technician is not shown an action the server
will refuse.
