---
number: 81
title: One composition root, and where each test lives
---

# ADR-081 — One composition root, and where each test lives

## Decision

**[`internal/app`](../../server/internal/app) is the only place a port is
wired.** Every other package receives what it needs. One place to read to know
what the server is made of, and one place a test can assemble a real server.

**An acceptance tier with two doors.** `server/tests/acceptance` drives the
product the way its two users do — the HTTP API and the agent control path —
with one outcome per product chapter, so a chapter with no acceptance test is
visible as a gap.

**The binding between chapters and tests is checked both ways.** A chapter with
no test fails, and a test naming no chapter fails.

**The tiers have a stated seam.** `server/tests/integration` holds what needs a
transport; `server/tests/acceptance` holds what states a product outcome; unit
tests hold the rest. A test in the wrong tier is a gate failure, not a matter of
taste.

**Real machines in the browser stack.** The compose stack runs agents, so the
browser suite exercises a session against something that answers.

## Consequences

CI runs the whole tree rather than a named list, so a new test directory is
picked up without anyone remembering to add it.
