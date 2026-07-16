---
adr: 055
title: Fault-Injection Mechanism — No Fault Code in the Shipped Binary
status: Accepted
date: 2026-07-15
---

# ADR-055: Fault-Injection Mechanism — No Fault Code in the Shipped Binary

## Status

Accepted.

## Context

Post-teardown, OpenGate runs one server replica with a local relay registry, an
in-cluster PostgreSQL, and a single shared free-tier ARM worker (the multi-replica
Redis/Sentinel/peer-proxy machinery was deleted — see
[`dormant-scale-out-teardown.md`](../../.claude/plans/archive/dormant-scale-out-teardown.md)).
It needs a repeatable fault-tolerance harness at two boundaries — the in-process
hexagonal ports (repositories, relay, agent control-write, middleware) and the
deployed infrastructure (ingress, the pod, rollouts, the QUIC/UDP agent path and
its network) — that produces deterministic evidence and gates promotion.

The hard constraint is that a device-management control plane must ship **no
fault code in the production binary**: production and staging must run the
identical image, and there must be no chaos endpoint or fault flag reachable in a
live server.

## Decision

**No fault code is compiled into the shipped server. Faults come from two
disjoint external places.**

- **In-process app-behavior faults → a Go test harness (adapter substitution).**
  Panic recovery, typed port-error mapping, request timeout, bounded blocking
  (ctx-cancel), connection-close, and tenant-context preservation are exercised by
  Go tests that start the real server in-process and substitute a
  fault-decorating port — the existing
  [`store_failure_test.go`](../../server/internal/api/store_failure_test.go)
  idiom, plus the [FI0 `AgentControl`](../../server/internal/api/api.go) seam for
  the agent control-write path. This code lives only in `_test.go` and runs in
  `make test` / normal CI.
- **Deployed faults → Chaos Mesh, on-demand and staging-scoped.** Dependency
  latency/abort, the QUIC/UDP agent path, packet loss/corrupt/partition (D1),
  pod deletion, and CPU/memory stress are injected by Chaos Mesh installed for a
  drill and uninstalled after. Edge 5xx/timeout is ingress annotations.

Rejected alternatives:

- **A compiled-in injector** (fault logic in the live server) — puts fault code
  and a selection surface in the production image; violates the same-image
  constraint outright.
- **A build-tag fault binary** — a non-shipping app variant breaks same-image
  promotion (staging would run a different binary than production).
- **toxiproxy** — TCP-only, so it cannot fault the QUIC/UDP agent path; Chaos
  Mesh already covers the TCP dependency hops, so toxiproxy adds a proxy without
  covering the gap that matters.
- **A separate Always-Free OKE cluster for chaos** — infeasible. The 200 GB
  block-volume cap is already consumed by four 50 GB minimum-size volumes (prod
  Postgres, VictoriaMetrics, Loki, node boot); a second cluster's mandatory
  ≥50 GB boot volume exceeds the cap (compute has headroom, storage is the wall).
  See [ADR-035](./ADR-035-oke-free-tier-block-volume-remediation.md).

**Accepted risk: a privileged chaos-daemon on the shared production worker.**
Because a second cluster is infeasible, Chaos Mesh's `NetworkChaos`/`StressChaos`
daemon runs on the one shared worker, where it cannot be scheduled away from
production. This risk is accepted under a hard, deliverable guardrail contract:
install/uninstall **on-demand** per drill (no standing daemon), **namespace-scoped**
to `opengate-staging` (`clusterScoped=false`, dashboard off), **every experiment
carries a `duration`**, **staging-only selectors plus a guard that forbids
selecting production pods** and refuses any namespace but staging, a **pinned
arm64 image digest**, and a **zero-residue** uninstall verified even on failure
(`trap` + `always()`). `PodChaos` uses the k8s API only and needs no daemon.

**D1 network chaos is in scope** — packet loss/corrupt/reorder/partition and the
QUIC/UDP agent path, delivered via `NetworkChaos` under the same guardrails.

## Consequences

- The production image contains zero fault code by construction; the absence of a
  compiled-in injector is asserted structurally (a no-import architecture rule in
  the fault suite), so there is no disabled-overhead benchmark to run.
- Deployed faults are **coarse** — per pod / netns / ingress host, not
  per-request. Per-request in-deployment isolation is parked; the Go harness
  proves isolation per-test instead.
- Both CI surfaces gate promotion together: the Go fault suite in normal CI and
  the Chaos Mesh infra/network drills + ingress edge tests as a required post-E2E
  staging stage, with no clean-run-history waiting period. Their determinism is
  therefore mandatory — a flaky drill blocks deploys.
- A failed Chaos Mesh uninstall is a release-blocking condition caught by the
  `always()` zero-residue check.

The full fault catalog, harness action set, Chaos Mesh guardrails, per-scenario
outcomes, and recovery-SLO budgets are specified in
[Fault Injection](../Fault-Injection.md).
