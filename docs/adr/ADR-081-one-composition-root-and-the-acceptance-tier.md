---
adr: 081
title: "One Composition Root, and an Acceptance Tier Bound to the Product Chapters"
status: Accepted
date: 2026-08-20
---

# ADR-081: One Composition Root, and an Acceptance Tier Bound to the Product Chapters

## Status

Accepted.

## Context

The test suite was wide and fast in the middle — 220 tests in
`internal/api` driving the routed server against a real database, reaching 59 of
the 63 paths the spec declares, plus 82 more in `server/tests/integration` — and
it still could not answer the question a customer asks. Five things were
measured before this decision.

**No test stood the product up.** `api.ServerConfig` declares 44 fields, and the
tree held 41 separate composite literals of it. Production wired 41 fields; the
richest test wired 22; the default harness wired 18. Seven agent-side ports were
wired in production and by no test at all. The divergence was silent, because
chi's recoverer turns a missing port into a 500 rather than a crash: a route
whose port a harness forgot answered 500, and nobody wrote a test against it.

**The seams that carry the product's value were joined by nothing.** Nothing
referenced the enrolment endpoints, so the chain a real machine walks — token
minted, certificate signed, agent connected, device visible — was proven by no
test. `MetricWindow` appeared in no joined test at all: the write half was tested
against in-process fakes, the read half against a fake reader, and the metrics
client against a real store. Four halves, no whole. Commit `532f7abe` is what
that costs: the ingest counter fired before any handler decided whether it had
anything to persist, and the fleet measured thousands of metric windows
received, counted, never written, and never dropped, with every tier green
throughout.

**The browser suite could not begin at a machine.** The stack ran a server and a
database and no agent, so nine of twenty-one specs reached for `page.route` and
six fabricated a whole machine — its device row, its hardware, its inventory, its
sessions. Sixteen tests asserted that the browser renders a tab against a server
that was not there.

**Thirty-two tests never ran in CI.** The gauntlet ran the whole `server/tests/`
tree; CI ran one package of it. Four packages — `vmramseries`, `loadtest`,
`vmcardinality`, `vmbackfill` — passed locally and were measured by no pipeline,
and a new package under `server/tests/` inherited that hole the moment it was
created.

**The two Go middle tiers had no stated seam.** Classifying every test in
`server/tests/integration` by what it needed: 25 needed a real QUIC peer, 14 a
real socket or WebSocket, and 47 needed neither. Those 47 were unit tests at an
integration address, because nothing said where a test belonged and each new one
followed the last.

## Decision

**One composition root.** [`internal/app`](../../server/internal/app) assembles
the product from resolved configuration: every repository, every port, both
servers, and the periodic workers that run on a timer rather than on a request.
It reads no flag, reads no environment variable, calls no `os.Exit` and opens no
listener. `cmd/meshserver` reads the world, chooses the cadence those workers
run on, and starts things; it wires nothing. A configuration short of something required fails `Build` naming the
missing dependency, rather than assembling a server that answers 500 on the
routes that needed it.

That split is what makes the package coverable: a `Build` free of flags and
exits is exercisable to the last line by the harness that calls it, and
`internal/app` falls inside the Go coverage gate. It is mutated rather than
carved out — its mutants land on the refusals, the all-or-nothing wiring of the
metrics store's four faces, the fallback that leaves device deletion as a plain
Postgres delete, and the schedule a worker is refused for leaving incomplete —
so it owns a mutation shard of its own rather than inheriting
`cmd/meshserver`'s exclusion.

**An acceptance tier with two doors.**
[`server/tests/acceptance`](../../server/tests/acceptance) stands the whole
product up over `app.Build` and speaks through exactly two doors, because a real
installation has exactly two: a technician at the HTTP API, and a machine on the
QUIC control stream. A test that reaches past them into a repository is
asserting on a row rather than on anything a customer can see. The one exception
is arranging a precondition the product offers no door for, and each such helper
is named `arrange…` so it is visible in the test's own text.

The Go `Machine` is not a substitute for the Rust agent; it is a peer on the same
wire, and the bidirectional golden fixtures
([ADR-016](ADR-016-bidirectional-goldens-and-sidecars.md)) are what make that
claim true rather than hopeful.

Real Postgres everywhere. The only doubles are the three edges that sit outside
the product: Intel management hardware, which answers on its own network path;
browser push delivery; and the GitHub release feed. A hand-written repository
double drifts from real semantics — `ON CONFLICT`, ordering, isolation, exact
error values — which is a fidelity loss this product cannot take, and `testpg`
already makes a real database unconditional
([ADR-029](ADR-029-test-determinism-no-silent-skips.md)).

No Gherkin and no scenario layer. The domain is technical and its readers are
engineers, so a business-readability framework buys nothing and costs a
dependency; the outcome is the test's name and the domain language is in its
assertion messages.

**The capability binding gates in both directions.** Every chapter of
[`docs/product/`](../product) is bound to the outcomes that prove it. The guard
reads the chapter directory and the package's own syntax tree: a chapter with no
outcome fails the suite naming the chapter, an outcome naming no chapter fails it
the other way, and a named test that does not exist fails it too. Reading the
directory rather than a committed list means a docs commit can turn the suite
red — which is the intent, and the alternative is a hand-kept list that rots
silently.

**A stated seam between the Go tiers.** `server/tests/integration` holds what
needs a transport and nothing else; everything else lives beside the code it
exercises, black-box where it was black-box. The 47 transport-free tests moved:
the pgx type semantics to `internal/db`, the signaling tracker to
`internal/signaling`, the dependency-graph guard to `internal/faulttest`, and the
plain HTTP tests to `internal/api` as an external `api_test` package. The rule is
enforced by
[`test-tier-placement.test.sh`](../../scripts/tests/test-tier-placement.test.sh),
so a reader choosing where to put a new test has a rule rather than a precedent.

**CI runs the whole tree.** The Go integration job runs `./tests/...`, with
shared Postgres and VictoriaMetrics provisioned for it, and
[`go-test-scope-parity.test.sh`](../../scripts/tests/go-test-scope-parity.test.sh)
holds CI's package patterns equal to the gauntlet's so the two cannot drift
apart again.

**Real machines in the browser stack.** The compose stack runs two agents with
pinned hostnames, built from the existing `x86_64-unknown-linux-musl` binary on
a bare Alpine. They install the way a real machine does: the bring-up mints an
enrolment token through the public endpoint, each agent generates its own key and
asks to be signed, and connects. No private key is copied and no test-only
affordance enters the shipped server. Linux agents support Terminal and File
Manager, and desktop capture there is the null implementation in production as
much as in a container, so a Linux container is not reduced fidelity — it is what
a Linux endpoint is.

## Consequences

The number of places a port is wired goes from 41 to one. A new field on
`ServerConfig` is wired once, and a harness cannot forget it.

A capability added to the product without an outcome test fails the suite rather
than being noticed months later. The readings round-trip is asserted from the
machine's report to the technician's read over a real metrics store, which is the
shape that goes red for the defect that shipped; an accounting invariant would
balance perfectly against a store that accepted a reading and a reader that
returned nothing.

The triage path is closed through HTTP: the repository's one deliberately joined
test called the store directly on the technician half, so a refusal on an
authorisation or tenancy ground would have left it green.

Sixteen browser tests stopped being fiction without the suite growing: its test
count is unchanged. The e2e job gains a dependency on an agent-binary job that
builds the static binary once inside the Rust cache and hands it over as an
artifact, which keeps the compile off the e2e job's critical path.

One thing is stated rather than fixed, because fixing it is a larger decision
than this one. The Chat tab is shown only for a machine reporting RemoteDesktop,
which a Linux agent does not, so `chat.spec.ts` keeps a described machine and
says why in its own header.

The periodic workers are the assembled product's, started through an `Assembly`
method on a schedule the caller states. The binary runs them on the cadence a
running server keeps; an acceptance test states its own and watches an orphaned
session row actually get reclaimed, which is an outcome the two doors can see
rather than one asserted by a sweep's unit tests and nothing joined.
