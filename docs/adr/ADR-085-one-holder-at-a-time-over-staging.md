---
adr: 085
title: One Holder at a Time Over the Staging Namespace
status: Accepted
date: 2026-08-27
---

# ADR-085: One Holder at a Time Over the Staging Namespace

## Status

Accepted.

## Context

Three workflows drive the `opengate-staging` release. The staging deploy
([ADR-084](./ADR-084-staging-e2e-runs-against-real-machines.md)) rolls the
server, truncates the database, and creates and removes two machines. The
nightly load run ([ADR-082](./ADR-082-load-runs-measure-the-system-or-say-they-did-not.md))
reads that same server, mints against an account the truncate removes, and puts
its own generator pods on the one Always-Free node the staging database shares
with production. The fault drill deletes pods there on purpose.

Nothing kept them apart. Each sits in a GitHub concurrency group of its own —
`deploy`, `load-test`, `fault-tolerance-staging` — and groups only serialise
what is inside them, so the schedules were free to overlap. Three collisions
followed from that: a load run starting inside the deploy's truncate found no
administrator to mint against; a load run waiting 120 seconds for a rollout the
deploy allowed 300 gave up first and reported the deploy's rollout as its own
failure; and generator pods asked a node for room while the deploy's machines
were still on it.

A single concurrency group shared between them is the obvious answer and is the
wrong one, for two reasons that are properties of the mechanism rather than of
the configuration:

- **A group is held for as long as the workflow sits in it, including while it
  waits for a human.** The `staging` and `production` environments both carry
  required reviewers, so a deploy is queued from the moment it is created and
  starts only when it is approved — a wait that has been observed at just under
  three hours and at thirty. A nightly sharing that group would be held for the
  whole wait, not for the ten minutes of work.
- **A group holds one pending entry, and a third arrival cancels it.** With
  `cancel-in-progress: false` the *running* entry is safe; the *pending* one is
  not. Two deploys in an evening would therefore cancel the queued nightly
  outright, and a scheduled run is never retried — the night's measurement would
  disappear with nothing but a cancelled run to show for it.

A group also covers only what GitHub starts. A `workflow_dispatch`, or a
`kubectl` somebody types, is outside it.

## Decision

**The lock lives where the state does.** A `coordination.k8s.io/v1` Lease named
`opengate-staging-guard`, in the namespace it protects, is acquired by each of
the three workflows before it touches anything and released afterwards in a step
that runs whatever the verdict was. [`scripts/staging-lease.sh`](../../scripts/staging-lease.sh)
is the single implementation all three call.

**A claim expires on its own.** A holder records how long its claim outlives its
last write, and a waiter treats a claim past that as free — so a job killed
mid-run frees the namespace without anyone clearing it by hand. The staging
deploy job is bounded below the lease it takes, so it cannot outlive its own
claim and have it taken while it is still writing.

**Taking over is a compare-and-set.** A waiter that finds an expired claim
replaces it carrying the resource version it read, so two waiters racing for the
same dead claim cannot both win. An unheld namespace is claimed with a create,
which the API server refuses if another holder got there first.

**An unreadable cluster is not an unheld namespace.** Only a "not found" answer
counts as free; any other failure stops the run. Reading an outage as an empty
namespace would hand the lock to every waiter at once.

**The lock is not the only thing standing between the load run and a red
night.** The run seeds the administrator it mints against from the same file the
chart's post-upgrade hook reads, rather than trusting a deploy hours earlier to
have left one behind, and it waits on a rollout as long as the deploy does. Both
hold on their own if the lease is ever not held — which is what makes the lease
a guard rather than a single point the nightly depends on.

## Consequences

The lease is held only while work is actually running, so a deploy waiting on
its reviewer blocks nothing. A run that cannot get in fails loudly, naming the
holder, rather than being silently cancelled.

Manual work is covered on the same terms as the schedules, because the lock is
an object in the cluster rather than a property of a workflow run.

A holder that dies blocks the namespace until its claim ages out. The holder
identity carries the run it belongs to, so the wait names what to look at.

No permission change was needed: the identity the deploy already uses creates
namespaces and secrets in this cluster, which is strictly more than a lease
requires.
