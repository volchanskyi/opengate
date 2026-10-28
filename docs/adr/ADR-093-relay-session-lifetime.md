---
number: 93
title: The relay owns a session's lifetime
---

# ADR-093 — The relay owns a session's lifetime

## Context

A WebSocket handler that hijacks its connection is left holding a request
context that will never be cancelled, because the request it belonged to is
over. A handler parked on that context waits forever.

That is what happened: every finished relay session stranded two goroutines and
the memory they held. Staging climbed 29, 134, 230 and 334 MiB across four
nightly load runs and was killed at its limit, holding 7,148 goroutines with no
sessions live. Every number the server published about itself read healthy the
whole time, because the code that decrements those numbers ran fine — it was the
handler that did not finish.

## Decision

**The relay says when a session has ended.** `Register` hands the caller a
channel that closes when the relay is done with that session.

**The handler parks on that channel and on a server-lifetime context, never on
the request context.** It closes its own WebSocket on the way out with
`CloseNow`, books the session out before it waits on the network, and shutdown
waits for the relay to drain.

**A session checks that its peer is alive rather than assuming it.** A forwarded
frame carries a write deadline; a read does not, because a quiet technician is
not a dead one.

**A count of a resource is paired with a reading of the resource.** The count
says the teardown code ran. Only a reading says the resource came back. The gate
is a slope: [`conservation_test.go`](../../server/tests/integration/conservation_test.go)
drives a growing number of complete sessions against one server, fits a line
through goroutines and retained memory against sessions completed, and requires
both slopes flat. A fixed baseline would have to guess at what the process holds
at rest and would go stale on the next change; a slope states the property
directly — a finished operation gives back what it took. Each test states its
tolerance beside the two readings that bracket it: what the defect read, and
what the fixed code reads.

[`mps_conservation_test.go`](../../server/tests/integration/mps_conservation_test.go)
holds Intel AMT requests the same way, a third of them answered, a third stalled
after the channel opens and a third never confirmed. The relay and the Intel AMT
path are what the conservation tests drive; the agent's QUIC path has no
conservation test of its own, and the static rule below fires on a new
long-lived connection path before a test has to.

A static rule
([`hijacked-request-context.yaml`](../../policy/semgrep/resources/hijacked-request-context.yaml))
refuses the shape at commit time, so the test is the second line rather than the
first.

## Consequences

Nothing else could have caught it. Coverage counted the leaking line as covered
because it ran. The benchmark measures allocations per operation, which a leak
does not change. Mutation testing found a copy of the tree with the line deleted
passed every test, slightly faster.

The rule is written down in
[`resource-conservation.md`](../../.claude/rules/resource-conservation.md).
