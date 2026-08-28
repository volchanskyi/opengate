---
adr: 086
title: The Cluster Is the Source of Truth for What Is Deployed
status: Accepted
supersedes: 025
date: 2026-08-28
---

# ADR-086: The Cluster Is the Source of Truth for What Is Deployed

## Status

Accepted. Supersedes
[ADR-025](./ADR-025-cd-preflight-digest-check.md).

## Context

The deploy's pre-flight decides whether staging needs rolling at all: if the
image digest it is about to deploy is the one already there, and `deploy/**` has
not changed since, the rollout and the browser suite are skipped. It is worth
having — a census of a hundred and two runs found it firing on 45.8% of the runs
that reached a decision, each saving about fourteen minutes.

It stood on a note the previous successful deploy left in the Actions cache: the
digest, the commit, the tag. That note stopped being written on 2026-06-30. The
deploy's cache token carries read scope only, so every save it attempts is
refused with a warning, and the step that attempts it reports success. Restores
still worked, which is why nothing looked wrong: the workflow was reading an
entry that had stopped being replaced, until it aged out and the skip rate went
to zero. Two months passed. No commit of ours is inside the window in which it
changed.

A probe settled what does and does not decide the scope. A `workflow_run` job
one level behind a `push` on `main` writes, so the branch an upstream ran on is
not it; every writer measured is one level behind a `push`, and the deploy is
two levels behind one with a `workflow_run` of its own as its upstream. Which of
those two it is remains open, and a trusted first-level trigger would be worth
trying. This decision does not depend on the answer.

Two properties of the cache made it the wrong place for this regardless of the
scope question. A cached note is a claim about the cluster written by something
that is not the cluster, so a manual intervention leaves it stale and the skip
fires against a state that no longer exists — ADR-025 listed that as an accepted
trade-off with a manual reconciliation. And a note that stops being written
degrades into silence rather than into an error.

## Decision

**The pre-flight reads the running deployment.** `resolve-tag` fetches a
kubeconfig, reads the image reference off the staging server container, and
takes both halves it needs from the tag on it: the `sha-<7>` tag resolves to the
digest through the registry, and its seven characters are the commit the
`deploy/**` diff runs against. Nothing is cached and nothing is written back.
A claim about what staging is running cannot be stale when staging is what
answered it.

Every branch still fails open, and the set of them grows by one: a manual
dispatch, an unresolved target digest, a `deploy/**` change, a cluster it cannot
reach, a release that is not there, and a running reference in any shape this
cannot read all fall through to the full rollout. The pre-flight is an
optimisation; when in doubt it deploys.

**The production skip stays as it is.** Production is gated twice —
`should_skip_staging != 'true'` and `needs.deploy-staging-k8s.result ==
'success'`, and a skipped job reports `skipped`. Removing the first clause would
change nothing, and ADR-025's description of production as outside the
pre-flight's scope described a workflow that was never built.

**The deploy declares no cache at all.** Its token cannot write, so every cache
it declares is a save that will be refused and reported as a success — including
the cache half of `setup-node`, whose restore was working and whose save was
not. What it needs instead it takes from elsewhere: the running state from the
cluster, and the agent binary as an artifact from the image workflow
([ADR-084](./ADR-084-staging-e2e-runs-against-real-machines.md)), whose token
does write.

**A cache write we name is read back.** Where a cache is still written under a
key we chose — the image workflow's agent build — a job asserts through the
cache API that the key exists, and fails when it does not.
[`ci-cd-determinism.md`](../../.claude/rules/ci-cd-determinism.md) carries the
rule and the gate that holds the workflows to it.

## Consequences

`resolve-tag` gains a kubeconfig fetch, measured at about thirty-six seconds,
on every run — against a skip worth fourteen minutes on nearly half of them. It
also gains OCI credentials on the runner for the length of one job, removed by
the same teardown every other job that takes them uses.

The pre-flight is now correct after a manual intervention rather than merely
recoverable from one: rolling staging back by hand changes the answer the next
run reads, so nothing has to be reconciled and no cache entry has to be deleted.

A refused cache write in the image workflow now fails a job, which stops the
deploy behind it until somebody looks. That is the intended cost. The last one
went unnoticed for two months.
