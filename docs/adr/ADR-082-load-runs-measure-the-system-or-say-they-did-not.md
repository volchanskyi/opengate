---
adr: 082
title: "A Load Run Measures the System, or Says It Did Not"
status: Accepted
date: 2026-08-21
---

# ADR-082: A Load Run Measures the System, or Says It Did Not

## Status

Accepted.

## Context

The nightly load test ran, published rows, and gated on them. Reading the code
against the pinned toolchain showed that a large part of what it reported was
not a measurement of the server.

**Gate rows that could never evaluate.** The extraction in
[`loadtest-summarize.sh`](../../scripts/loadtest-summarize.sh) guarded its relay
row on the k6 v0.x summary shape, while the workflow pins k6 v1.x, which writes a
metric's statistics flat on the metric object. The row therefore never appeared,
and [`loadtest-regression-check.sh`](../../scripts/loadtest-regression-check.sh)
held three ceilings on a series that never arrived. The test that should have
caught it encoded the same wrong shape, so it stayed green.

**A measurement that was structurally zero.** The harness stopped its
registration clock immediately after handing the register frame to the QUIC
stream — a write into a local send buffer. The device row is written later, on
the server. Two ceilings sat on a number that could not move.

**A relay metric filled from a health check.** The relay scenario had no
WebSocket in it at all: it timed an unauthenticated `GET /api/v1/health` and
wrote the result into `relay_msg_latency_ms`. Its threshold comment described a
`kubectl port-forward` tunnel the workflow had stopped using.

**A partial night absorbed as data.** The k6 half discarded the export of an
aborted scenario. The QUIC half had no such rule, and nothing recorded which
scenarios produced rows. A night where one half ran and the other produced
nothing entered the trend indistinguishable from a slow system — and, being
lower, pulled the window median down, so the next genuinely slow night compared
favourably against it and passed. One partial night cost two.

**Residue.** Every user in the staging database was load-test residue: 81
accounts, all 81 matching a load-test address. No run had ever removed what it
made, and nothing counted.

**A fleet-wide credential on a shared runner.** The workflow copied the
certificate authority's private key out of the cluster to sign agent
certificates. That key mints a trusted machine for the whole fleet and no
rotation story covers it.

**A telemetry shape production does not send.** The harness emitted 13 metric
dimensions against the 18 the server stores, and named its anomaly families
`memory` and `network` where the server accounts for `mem`, `net` and `proc`.

**An observer slower than the workload.** The runtime gauges refreshed every 15
seconds behind a 30-second scrape — up to 45 seconds of lag — and a QUIC run
finishes inside that window, so `agents_connected` never left zero.

**Two free-tier guards that disagreed.** `compute.rego` denied above 4
processors / 24 GB; the Terraform guards in the `oke` and `compute` modules had
been moved to 2 / 12. The looser gate would admit a plan the stricter one
refuses, and which gate runs decides whether the plan lands.

## Decision

**A run has three outcomes, not two.** Valid and failed are both measurements —
one of a system that held, one of a system that did not, and both belong in the
trend. Invalid is the third: the run did not measure the system, because a
scenario produced no rows, the generator ran out of room, or a safety ceiling
stopped it. An invalid run never moves a window median.

**Every gate row is provably reachable.** A test reads the series triples out of
the regression check's own case labels and asserts the extraction produces each
one, from fixtures in the shape the *pinned* k6 writes. The fixture is tied to
the pinned version, so a version bump that changes the schema reaches the test.

**Registration is timed where the device row lands.** The server records the
outcome and duration of every registration; the harness reads that rather than
timing its own send buffer. Database-pool occupancy is published beside it,
because a slow registration queued behind a connection and one executing slowly
are the same latency until the pool says otherwise.

**The relay is measured through a relay.** The generator opens the operator's
side of a real session and times its own frame coming back; the Go harness holds
the machine's side and echoes. The two run concurrently, and the harness holds
its fleet connected for the whole window, because a session needs a machine on
the other end of it.

**A run is configured by one versioned profile and produces one versioned
bundle.** The profile declares phases, safety limits and gates; the bundle
carries enough to interpret its own numbers without the system that produced
them. The metrics store keeps 30 days, so the bundle is authoritative and the
dashboard is a view of it. A bundle missing a mandatory section fails the run.

**Offered load and achieved load are separate fields.** Collapsing them hides
the one case the validity rule exists for: a generator that could not produce
the load reads exactly like a system that could not absorb it.

**Expected rejections are counted apart from faults.** A refused write past a
declared limit is the system working, and counting it as a defect buries the
real ones.

**No certificate authority key leaves the cluster.** The harness enrols the way
an installer does — it keeps its private keys and sends signing requests — using
a token minted for the run and deleted after it.

**A run removes what it made and proves it.** Every identity carries a marker,
cleanup selects on it, and the count found afterwards travels in the bundle.

**Production is unreachable by construction.** The environment vocabulary has no
production member, and every address the harness dials — configured or arriving
on the wire in a session request — goes through one allowlist.

**Two families move to a disposable runner stack.** Volume varies how much data
is already there, and staging's database writes into the same node root
production depends on. Scaling varies processor count, and the cluster offers
one figure. A runner brings its own disk and processors and is the only venue
where the generator and the target hold genuinely separate boundaries — which is
what makes the tightened API mark meaningful rather than cosmetic. A runner is
x86_64 and production is ARM64, so those families produce comparisons and never
absolute capacity claims.

**The tightened mark is advisory until the spread narrows.** A breached
threshold no longer fails the scenario; it is recorded, and whether it fails the
run is the profile's decision. A mark set tighter than the measurement's own
spread would otherwise fail every night from the day it was tightened, which
teaches everyone to ignore it.

**The two free-tier guards are held equal at the stricter pair.** Agreement is
reached without widening what either gate allows.

**No storage decision until the fixture is weighed.** The block-storage grant is
exactly full, so giving staging its own disk costs another volume, and the only
candidate holds every log the fleet has. Whether a 2,000-machine fixture fits
inside the node root's margin is a fact one measurement settles.

## Consequences

The nightly gates on rows that exist. A partial night is recorded as invalid and
excluded rather than quietly lowering the bar for the next one. Registration and
relay figures describe the server. The staging database holds people's accounts
rather than a run's leftovers, and no fleet-wide credential is copied onto a
shared runner.

Two things stay open and are recorded as debt rather than closed by assumption:
load-test identities live in the default tenant because no tenant-creation API
exists, and whether the Always-Free processor grant is 2/12 or 4/24 needs the
OCI console. Neither blocks anything here; the storage question stays closed
until a measured fixture weight reopens it.

## References

- [`docs/infrastructure/Testing.md`](../infrastructure/Testing.md) — how the
  families are run and where each one runs.
- [ADR-035](ADR-035-oke-free-tier-block-volume-remediation.md) — the
  block-storage grant this plan's venue choices are bounded by.
- [ADR-065](ADR-065-vitals-contract-cadence-extrema-and-bounded-dims.md) — the
  vitals contract the harness's telemetry shape is held equal to.
