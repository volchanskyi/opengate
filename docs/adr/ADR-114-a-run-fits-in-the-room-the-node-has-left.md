---
number: 114
title: A run fits in the room the node has left, and a pod that does not says why
status: Accepted
date: 2026-09-10
---

# ADR-114 — A run fits in the room the node has left, and a pod that does not says why

## Context

The nightly network drill stood up four pods, ran twelve steps of setup —
building the shaper and the harness, minting credentials, starting the real
machine and waiting for it to come online — and then measured nothing.
[Run 34464514567](https://github.com/volchanskyi/opengate/actions/runs/34464514567)
took five and a half minutes to do it.

Its fleet pod was never scheduled. The cluster said so plainly, in an event
attached to the pod: `0/1 nodes are available: 1 Insufficient cpu`. Nothing read
it. What reached the log was `error: timed out waiting for the condition on
pods/netdrill-fleet-…`, and then, one step later, `netdrill-fleet-… did not
answer whether it holds a fleet, so this run will not start a second one over the
first one's fixture` — a sentence about a pod that might be holding a fleet,
written about a pod that had never been placed anywhere.

The two halves are separate defects and each is worth closing.

**The arithmetic.** The one worker node offers 1,830 millicores, and the
cluster's standing tenants — the ingress controller, the monitoring stack, OKE's
own agents, both databases and both servers — have reserved 1,680 of them. That
leaves 150. The drill's four pods asked for 185: ten for the probe, fifty for the
shaper, twenty-five for the real machine and a hundred for the fleet. The fleet's
is the last to be created, so the fleet's is the one refused. Every one of those
numbers is text in this repository and nothing anywhere added them up.

A hundred millicores was never what the fleet needed. It holds twenty simulated
machines sending a heartbeat every fifteen seconds — less work than the real
agent beside it, which reserves twenty-five.

**The report.** `kubectl wait` answers a pod that never became ready with a
timeout and nothing else, and the run is about to tear the cluster down. The
reason existed, in the cluster, for the whole of the two minutes the wait spent
looking at it.

## Decision

**A pod reserves what it uses, and the total a run reserves is held under what
the node has left.**

The fleet pod's reservation drops to the twenty-five millicores the real machine
beside it takes.
[`staging-node-budget.test.sh`](../../scripts/tests/staging-node-budget.test.sh)
sums each workflow's pod reservations — the ones written inline and the ones
written in the manifests those workflows apply — and fails when a workflow asks
for more than the remainder. The three workflows that create pods there each take
the staging claim before creating anything, so only one is ever on the node and
each is judged alone.

The remainder is a reading, taken off the live node and written down with the
command that re-takes it. It moves when the cluster's standing tenants move, and
a number that has to be re-read is better than four numbers nobody adds up.

**A wait that fails prints what the cluster says about the pod.**
[`wait-for-pod-ready.sh`](../../deploy/scripts/wait-for-pod-ready.sh) is now
every such wait in every workflow. It names the pod's phase — Pending is the
scheduler, anything else is the kubelet or the container — and then prints the
pod's own account, which carries the events the scheduler and the kubelet wrote.
A pod that is not there at all is named as absent rather than reported as a
timeout.

## Consequences

The drill reserves 110 millicores of the 150 available, the load test 150, and
the deploy's end-to-end machines 50. The load test sits exactly on the line,
which is the useful thing to know about it: the next pod anybody adds to that
workflow fails this check rather than a night.

The gate is a statement about reservations, not about use. A pod may burst to its
cap and several of them do; what the scheduler reads is the reservation, and the
reservation is what decides whether a pod is placed at all.

The wait says why in every workflow that waits, including the deploy's. That is
the half that generalises: the drill's night was lost not because a pod was
refused but because the refusal was three steps removed from the message anyone
read, and every wait in the repository had the same shape.
