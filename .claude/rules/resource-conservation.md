# Resource Conservation — A Counter Is Not A Measurement

**Enforced by:**
[`server/tests/integration/conservation_test.go`](../../server/tests/integration/conservation_test.go)
(gauntlet go-integration step),
[`policy/semgrep/resources/hijacked-request-context.yaml`](../../policy/semgrep/resources/hijacked-request-context.yaml)
(pen-test gate, fixtures pinned by
[`pentest-review.test.sh`](../../scripts/tests/pentest-review.test.sh)).
**No bypass.**

Companion to [`ci-cd-determinism.md`](ci-cd-determinism.md), which says a step
whose work was refused must not report success. This says the same of a resource:
a number saying the work was undone is not evidence that it was.

A relay handler parked on a request context a WebSocket hijack had left
uncancellable, and every completed session stranded two goroutines and their
retained heap. Staging walked 29 → 334 MiB across four nightly runs and was
killed against its limit at 7,148 goroutines with **zero** sessions live. Every
liveness number the server published was correct throughout, because the code
that decrements them ran — none of them is a reading of anything.

Each gate class was blind for its own structural reason: coverage counted the
leaking line as covered because it executed; the benchmark trend measures
allocations per operation, which are identical for a leak because only
*retention* differs; and a statement-level mutant of that line is equivalent
under every existing assertion.

## The rule

### A counter of a resource is paired with a reading of the resource

Wherever the product publishes a count it maintains itself — sessions,
connections, slots, grants, leases — something reads the resource the count is
about, and an invariant binds the two. The count answers *did my teardown code
run*. Only the reading answers *did the resource come back*.

### A path that acquires a per-session resource states where it is released, and a gate proves it

Naming the release site in a comment is the cheap half.
[`conservation_test.go`](../../server/tests/integration/conservation_test.go)
drives N complete operations against one assembled server at several values of N,
fits a line through retained goroutines and retained heap against completed
operations, and requires both slopes to be flat.

### The assertion is a slope, never a fixed baseline

Every `NewServer` starts goroutines that take no context and never stop, and the
store and its pool add more. A baseline has to guess at that constant and goes
stale the moment anything else in the process changes. A slope removes it, and is
the only form that states the property: *a completed operation gives back what it
took*.

A tolerance is stated beside the two measurements that bracket it — what the
defect read, and what the fixed code reads. A tolerance nobody can justify is a
flake waiting for a slow machine.

### The tier is the one that needs the transport

A conservation test drives real connections, so it lives in
`server/tests/integration/`, whose stated seam is what needs a transport
([`test-tier-placement.test.sh`](../../scripts/tests/test-tier-placement.test.sh)).

## Scope

The conservation test covers the relay today. Pointing it at the agent QUIC path
and the MPS path is this rule's job on its own schedule; a new long-lived
connection path inherits the rule when it is written, and the static guard fires
before the test has to.
