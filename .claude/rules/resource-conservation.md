# Resource Conservation — A Counter Is Not A Measurement

**Enforced by:**
[`server/tests/integration/conservation_test.go`](../../server/tests/integration/conservation_test.go)
(gauntlet go-integration step),
[`policy/semgrep/resources/hijacked-request-context.yaml`](../../policy/semgrep/resources/hijacked-request-context.yaml)
(pen-test gate, fixtures pinned by
[`pentest-review.test.sh`](../../scripts/tests/pentest-review.test.sh)).
**No bypass.**

Companion to [`ci-cd-determinism.md`](ci-cd-determinism.md), which says a step
whose work was refused must not report success. This says the same thing about a
resource: a number that says the work was undone is not evidence that it was.

## What it cost

A relay handler parked on a request context a WebSocket hijack had left
uncancellable. Every completed session stranded two goroutines and their
retained heap, forever. Staging walked 29 → 134 → 230 → 334 MiB across four
nightly load runs and was killed against a 384Mi limit, sitting at 7,148
goroutines with **zero** sessions live.

Every liveness number the server published was correct throughout.
`opengate_relay_active_sessions` returned to zero, `ActiveTokens` emptied, the
active count went back to zero — because the code that decrements them ran. Not
one of them is a reading of anything. They are bookkeeping the teardown path
maintains, and the teardown path was working; it was the handler that was not.

Nothing else could see it either, and each gate class was blind for its own
structural reason. Coverage counted the leaking line as covered, because it
executed. The benchmark trend measures allocations per operation, which are
identical for a leak, because only *retention* differs. Mutation testing mutates
conditionals and arithmetic, and a statement-level mutant of that line is
equivalent under every existing assertion — a copy of the tree with the line
deleted passed every test in all three tiers, marginally faster.

`runtime.NumGoroutine`, `go.uber.org/goleak` and `runtime.ReadMemStats` returned
zero matches across the whole repository. Across 43 gauntlet checks, 88 shell
tests and 25 CI jobs there was no conservation assertion in any language at any
tier.

## The rule

### A counter of a resource is paired with a reading of the resource

Wherever the product publishes a count it maintains itself — sessions,
connections, slots, grants, leases — something must read the resource the count
is about, and an invariant must bind the two. The count answers "did my teardown
code run". Only the reading answers "did the resource come back".

### A path that acquires a per-session resource states where it is released, and a gate proves it

Naming the release site in a comment is the cheap half. The gate is the half
that survives the next edit:
[`conservation_test.go`](../../server/tests/integration/conservation_test.go)
drives N complete operations against one assembled server at several values of
N, fits a line through retained goroutines and retained heap against completed
operations, and requires both slopes to be flat.

### The assertion is a slope, never a fixed baseline

Every `NewServer` starts goroutines that take no context and never stop, and the
store and its pool add more. A baseline has to guess at that constant and goes
stale the moment anything else in the process changes. A slope removes it, and
is the only form that states the property being asserted: *a completed operation
gives back what it took*. The method is
[`vmramseries`](../../server/tests/vmramseries/)'s, whose own comment explains
why a single reading divided by what is present answers a different question.

A tolerance is stated in the test beside the two measurements that bracket it —
what the defect read, and what the fixed code reads. A tolerance nobody can
justify is a flake waiting for a slow machine.

### The tier is the one that needs the transport

A conservation test drives real connections, so it lives in
`server/tests/integration/`, whose stated seam is what needs a transport
([`test-tier-placement.test.sh`](../../scripts/tests/test-tier-placement.test.sh)).

## Scope

The conservation test covers the relay today. Pointing it at the agent QUIC path
and the MPS path is this rule's job on its own schedule; a new long-lived
connection path inherits the rule when it is written, and the static guard fires
before the test has to.
