---
number: 96
title: A counter of a resource is not a measurement of it
status: Accepted
date: 2026-09-02
---

# ADR-096 — A counter of a resource is not a measurement of it

## Context

A relay handler retained two goroutines and their heap for every completed
session, forever. Staging walked 29 → 134 → 230 → 334 MiB across four nightly
load runs and was killed against a 384Mi limit while holding 7,148 goroutines
with zero sessions live. The mechanism is
[ADR-093](ADR-093-a-relay-session-lifetime-is-owned-by-the-relay.md)'s subject.
This one is about why nothing saw it for the life of the project.

**Every liveness number the server published was correct throughout.**
`opengate_relay_active_sessions` returned to zero. `ActiveTokens` emptied. The
relay's active count went back to zero. All of them, because the code that
decrements them ran — the teardown path was working; it was the handler that was
not. Not one of those numbers is a reading of anything. They are bookkeeping a
teardown path maintains, and they answer "did my cleanup code run", which is a
different question from "did the resource come back".

Every gate class was blind, each for its own structural reason, and the reasons
generalise past this defect:

- **Coverage** counted the leaking line as covered, because it executed.
- **The benchmark trend** measures allocations per operation, which are
  identical for a leak. Only *retention* differs, and nothing measured retention.
- **Mutation testing** mutates conditionals and arithmetic; a statement-level
  mutant of that line is equivalent under every existing assertion.

The decisive experiment was a copy of the server tree with the leaking wait
deleted outright, re-running every test in all three tiers whose name mentions
the relay. All three tiers passed, marginally faster, because the handlers
returned instead of being abandoned. Not one assertion in 1,519 Go test
functions held that line.

`runtime.NumGoroutine`, `go.uber.org/goleak` and `runtime.ReadMemStats` returned
zero matches across the whole repository. Across 43 gauntlet checks, 88 shell
tests and 25 CI jobs there was no conservation assertion in any language at any
tier.

## Decision

**A counter of a resource is paired with a reading of the resource, and an
invariant binds the two.** Wherever the product publishes a count it maintains
itself — sessions, connections, slots, grants, leases — something reads the
resource that count is about. This is the sibling of
[ADR-091](ADR-091-a-coverage-report-is-written-in-the-readers-coordinates.md)'s
read-back doctrine and of
[ADR-088](ADR-088-a-gate-measures-the-system-not-its-own-harness.md)'s *a gate
measures the system, not its own harness*.

**The assertion is a slope, never a fixed baseline.** Every `NewServer` starts
goroutines that take no context and never stop, and the store and its pool add
more. A baseline has to guess at that constant and rots the moment anything else
in the process changes. A slope removes it, and it is the only form that states
the property: *a completed operation gives back what it took*. The method is the
one [`vmramseries`](../../server/tests/vmramseries/) already uses, for the reason
its own comment gives — a single reading divided by what is present answers a
different question.

**The tolerance is bracketed by two measurements, both stated in the test.**
What the defect read, and what the fixed code reads. For the relay: 2.000
goroutines and 34 KiB per session with the defect, 0.000 and 1.8 KiB without it,
so the tolerances sit at 0.5 and 8 KiB. A tolerance nobody can justify is a
flake waiting for a slow machine.

**A static guard fires before the test has to.**
[`hijacked-request-context.yaml`](../../policy/semgrep/resources/hijacked-request-context.yaml)
refuses a handler in the API package's handler files that waits on the request
context. It is a genuine
[ADR-027](ADR-027-adversarial-pentest-precommit-gate.md) subject rather than a
stretch of one: an attacker who opens and abandons sessions consumes the server
without bound, which is CWE-400. It ships with both fixtures — one it fires on,
one it is silent on — because a rule nobody can trust to be silent is a rule
that will be suppressed.

**The rule is written down**, in
[`resource-conservation.md`](../../.claude/rules/resource-conservation.md), so
the next long-lived connection path inherits it rather than rediscovering it.

## Consequences

The conservation test costs a few seconds in the integration tier and fails
loudly on the pre-fix code, which is the property that makes it worth having: it
was written against a defect it can be shown to catch, not against a shape
somebody imagined.

Its scope today is the relay. Pointing it at the agent QUIC path and the MPS path
is the rule's job on its own schedule, not a defect this incident found — and
saying so is part of the decision rather than a gap in it.

The static guard is approximate in two stated ways: it is scoped by path to the
API package's handler files, because that is where this server hijacks
connections and the `Accept` usually sits in a different function from the wait;
and within that scope it treats any wait on a request context as this defect. A
handler there that legitimately blocks on cancellation would need the rule
revisited rather than suppressed, which is the trade
[`coverage-exclusions.md`](../../.claude/rules/coverage-exclusions.md) already
states for exemptions generally.
