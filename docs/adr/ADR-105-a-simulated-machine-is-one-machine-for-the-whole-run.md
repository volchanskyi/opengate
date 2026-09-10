---
number: 105
title: A simulated machine is one machine, for the whole run
status: Accepted
date: 2026-09-09
---

# ADR-105 — A simulated machine is one machine, for the whole run

## Context

The load harness models an estate of machines connecting to a server. Two
things it did were not what a machine does, and both distorted the events the
profiles exist to reproduce.

**It enrolled on every start.** `forAgent` minted a fresh identity per call, so
every machine a phase started created a new device row. A real machine enrols
once, when it is installed, and comes back afterwards with the certificate it
already holds — the server knows it by that certificate. Three consequences
followed, and they compound:

- A burst of two thousand reconnections was a burst of two thousand
  *enrolments*, against a rate the server refuses past on purpose. The
  250-machine staging rung came back `invalid` with
  `enrollment refused with 429: rate limit exceeded`, which was a correctly
  enforced limit doing its job against a load nothing in the field would ever
  produce.
- The fleet grew for as long as the run lasted, so a five-hour endurance run
  confounded its own question with the volume dimension: whatever it found in
  the last hour was found against a database several times the size of the one
  in the first.
- The event being modelled — a site's link coming back — was not the event
  being run. The two fail differently, which is the whole reason the spike
  family exists.

**It went quiet when its hold ran out.** `holdOpen` wrote a heartbeat on an
interval, which is the only thing that can tell a quiet server from a severed
one: the heartbeat runs agent to server, so a held machine receives nothing
from a healthy server either, and no amount of reading separates them. When the
declared hold elapsed the machine stopped writing and parked on the run's
context instead. It kept its connection — the server keep-alives every thirty
seconds against a ninety-second idle timeout, and a machine's online status
follows the connection — so nothing looked wrong. What it lost was
`ErrHeldPeerGone`, which is raised by the write. In a profiled run the blind
window is every minute past `-hold`, which for the nightly sweep is most of the
walk.

## Decision

**A machine's identity is minted once and reused, and no two live connections
are ever the same machine.**

The estate is a fixed roster
([`roster.go`](../../server/tests/loadtest/roster.go)). A start takes a machine
nobody is currently connected as and gives it back when it leaves;
`enrolOnce` memoises the credential by that machine's name. Reuse without the
roster would be worse than the defect it closes — one device on two live
connections is the server keeping whichever registered last, and the fleet's
level silently dropping by the one displaced.

An estate with nobody free reports that rather than doubling up, and a profile
declaring a level the estate cannot reach is refused before the clock starts.
The two outcomes are indistinguishable in a bundle — an attainment short of its
offer — and only one of them is a finding about the system.

This gives `-agents` a job it did not have: it sizes the estate the profile
draws from. It still does not decide how many machines connect; the profile's
`connected_agents` does.

**A machine proves its connection for the whole of its stay.** The two loops
became one, driven by whichever bound applies, so there is no window in which a
machine is in the fleet and not writing.

## Consequences

- A burst is a burst of reconnections. The enrolment ceiling is exercised by
  the arrival of an estate, once, rather than by every phase of every run.
- An endurance run holds its fleet constant while machines come and go, so what
  it measures is retention rather than growth.
- A severed fleet is detected for as long as the fleet is held, not for as long
  as `-hold` declared.
- A workflow that walks a profile has to give `-agents` at least the profile's
  highest level, or the run refuses at its first line. Every workflow that
  names a profile does.
- The phase probe takes one identity of its own rather than one per round trip.
  Numbering them enrolled a new device at every step of every ramp — over five
  hours, the fleet growing by the measurement of it.
