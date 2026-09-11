---
number: 84
title: The staging environment
---

# ADR-084 — The staging environment

## Context

The browser suite needs machines to act on, and several workflows want staging
at the same time.

## Decision

**Staging has two real machines, built and enrolled by the deploy that creates
them.** The suite is not narrowed to the specs that need no machine.

**Their binary is cross-built from the commit being deployed**, not pulled from
a release, so the suite tests what is being deployed.

**The name a machine dials is the name on the certificate.** The chart issues
the server's certificate for the in-cluster name the agents actually use.

**No authority key leaves the cluster.** Machines enrol the way an installer
does — they keep their private key and send a signing request, using a token
minted for the run and deleted afterwards.

**The bootstrap operator registers first** and nothing else takes that row, so
the first account is predictable rather than whichever test got there first.

**One copy of the seeding statements**, in a file the chart runs, rather than
the same SQL in the chart and in a workflow drifting apart.

### One holder at a time

**The lock lives where the state does** — a Lease in the cluster, named for the
namespace it protects. A GitHub concurrency group was rejected: a deploy waiting
for its human reviewer would hold it for hours.

**A claim expires on its own**, and the holder records how long its claim
outlives its work, so a crashed job does not lock staging until somebody
notices.

**Taking over is a compare-and-set**, so two waiters finding the same expired
claim cannot both win.

**A cluster that cannot be read is not an unheld namespace.** Only an explicit
"not found" means free. A refusal that is not a lost race is reported as a
refusal rather than counted as contention.

## Consequences

Staging is production-shaped, so a number measured there means something about
production. The lock is not the only thing between a load run and a red suite;
it is the thing that makes the two not each other's fault.
