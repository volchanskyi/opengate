# Resource Conservation — A Counter Is Not A Measurement

**Enforced by:**
[`server/tests/integration/conservation_test.go`](../../server/tests/integration/conservation_test.go)
(gauntlet go-integration step),
[`policy/semgrep/resources/hijacked-request-context.yaml`](../../policy/semgrep/resources/hijacked-request-context.yaml)
(pen-test gate, fixtures pinned by
[`pentest-review.test.sh`](../../scripts/tests/pentest-review.test.sh)).
**No bypass.**

Companion to [`ci-cd-determinism.md`](ci-cd-determinism.md). A number saying the
work was undone is not evidence that it was.

## The rules

### A counter of a resource is paired with a reading of the resource

- Wherever the product publishes a count it maintains itself — sessions,
  connections, slots, grants, leases — something reads the resource that count
  is about, and an invariant binds the two.
- The count answers *did my teardown code run*. Only the reading answers *did
  the resource come back*.

### A path that acquires a per-session resource states where it is released, and a gate proves it

- [`conservation_test.go`](../../server/tests/integration/conservation_test.go)
  drives N complete operations against one assembled server at several values of
  N, fits a line through retained goroutines and retained heap against completed
  operations, and requires both slopes to be flat.

### The assertion is a slope, never a fixed baseline

- A baseline has to guess at the constant every `NewServer` starts, and goes
  stale when anything else in the process changes.
- A tolerance is stated beside the two measurements that bracket it — what the
  defect read, and what the fixed code reads.

### The tier is the one that needs the transport

- A conservation test drives real connections, so it lives in
  `server/tests/integration/`
  ([`test-tier-placement.test.sh`](../../scripts/tests/test-tier-placement.test.sh)).

## Scope

- The conservation test covers the relay. The agent QUIC path and the MPS path
  are not yet covered by it.
- A new long-lived connection path inherits the rule when it is written, and the
  static guard fires before the test has to.
