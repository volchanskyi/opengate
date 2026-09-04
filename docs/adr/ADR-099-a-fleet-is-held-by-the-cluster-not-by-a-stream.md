---
number: 99
title: A fleet is held by the cluster, not by a stream
status: Accepted
date: 2026-09-04
---

# ADR-099 — A fleet is held by the cluster, not by a stream

## Context

The nightly load run needs a fleet of machines connected while the k6 scenarios
run beside it, because the relay scenario opens the operator's side of a real
session and a session needs a machine on the other end of it.

That fleet was held by one `kubectl exec` running for the whole window,
backgrounded on the GitHub runner. The command reached the pod over three hops —
runner, API server, kubelet — and every one of the eight minutes the fleet was
supposed to hold was a minute that stream had to survive.

Run 33856943325 is what it cost. The API server's request to the kubelet came
back `EOF` at the moment of the launch, so the harness never ran: no fixture was
built, and the cleanup pass afterwards removed nought customers, nought sites and
nought machines. The step that launched it slept forty-five seconds and reported
success, because a sleep cannot tell the difference. Two k6 scenarios then
measured a staging server with no fleet beside it and wrote their rows, and the
first thing to notice was the relay scenario failing four minutes later for want
of a machine to open a session against. The completeness gate scored the night
invalid, which is correct and is also the only correct thing that happened.

This is the failure class [`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md)
already names — a step whose work was refused reporting success — reached by a
route that rule had not covered: the step's work was not a cache write or an
artifact upload but a process, and the evidence that it had started was a
duration rather than an answer.

The k6 half of the same workflow had already learned the transport lesson and
stopped reaching staging through a `kubectl port-forward` tunnel, which was both
fragile and inside the measurement. The fleet was the last part of the run whose
life depended on a stream the runner owned.

## Decision

**The harness is launched detached inside the pod, and the pod holds the fleet.**
[`scripts/loadtest-quic-incluster.sh`](../../scripts/loadtest-quic-incluster.sh)
is the seam: `start` runs a launcher that detaches the harness, redirects its
output to a file in the pod and writes its exit code to another when it finishes;
`collect` reads both back afterwards. No call lasts longer than a moment, so no
single stream carries the run, and a call that does not arrive costs the answer
rather than the fleet.

**A start proves the fleet is up rather than sleeping until it probably is.**
`start` returns only once the harness's own announcement — the line it prints when
its fixture is built and it is about to offer its fleet — is in the pod's log. A
harness that reached a verdict without announcing anything ends the wait
immediately, and its own output is what the failure report carries.

**A launch that never happened is made again; a launch that happened is never
made twice.** The retry decision is taken by asking the pod what it holds, not by
reading the client's error text. The asymmetry is not caution for its own sake: a
second harness would build a second fixture over the first one's names, and a
customer's name is unique inside its tenant, so the duplicate run would be refused
partway through and leave the fleet it was sent to double. When the pod cannot be
reached to answer the question, the answer is not assumed — the run fails
naming what it could not ask.

**The keep-or-discard rule stays where it was.**
[`scripts/loadtest-quic-run.sh`](../../scripts/loadtest-quic-run.sh) still decides
whether the QUIC half measured anything; what it wraps is now the read-back rather
than the fleet itself. Its verdict is its exit code, which the caller waits on
directly.

## Consequences

A blip on the runner-to-kubelet path costs a retried call instead of the night.
The failure that remains — a fleet that genuinely will not come up — is reported
by the step that owns it, at the moment it is known, with the harness's own words,
rather than four minutes later through a scenario complaining about something
else.

The run also spends less of the job's clock on a lost cause: the collect step
asks the pod whether a fleet was ever launched before it starts waiting for a
verdict, so the path where the start already failed answers at once instead of
waiting out its bound.

The exemption this leaves standing is the fixture's own idempotence. A harness
that dies partway through building one leaves rows nothing removes until the
run's cleanup pass, and the retry rule declines to start a second harness for
exactly that reason. Making the fixture resumable would let a retry cover that
case too, and until it is, that case is reported rather than repaired.

[`scripts/tests/loadtest-quic-incluster.test.sh`](../../scripts/tests/loadtest-quic-incluster.test.sh)
drives the seam against a pod stand-in that runs the launcher for real and
refuses the calls the way the kubelet refused them, so the retry rule, the
readiness proof and the workflow's use of both are held by a test rather than by
this document.
