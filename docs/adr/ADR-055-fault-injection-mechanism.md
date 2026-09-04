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
- **Deployed faults → staging-scoped tooling outside the server, holding no
  privilege.** Pod deletion and bad rollout are runner scripts driving `kubectl`
  and `helm` directly. Edge 5xx/timeout is ingress-nginx annotations. The
  machine-facing QUIC path is faulted by an **in-path link shaper**
  ([`server/tests/netfault/`](../../server/tests/netfault)): the drill's machines
  dial the name on the server's certificate, a `hostAliases` entry points that
  name at the shaper, and the shaper forwards each datagram on with whatever
  impairment the scenario commanded — total outage, one-way loss, fixed delay, a
  shared bit-rate link, or a move to a new server-facing address.

  **This replaces the Chaos Mesh mechanism this decision originally named, which
  was never built.** Two things settled it, both measured on the live cluster:
  the worker node's kernel configures `sch_netem` as a module and the OKE image
  does not ship it — it is absent from `/lib/modules` and from `modules.dep`, so
  no container can supply it — and Chaos Mesh implements delay, loss, duplicate
  and corrupt as `netem` actions. It would therefore have delivered two of the
  five impairments above while adding a privileged node agent holding
  `SYS_ADMIN`, `SYS_PTRACE`, `SYS_CHROOT`, `MKNOD`, `KILL` and `IPC_LOCK` plus
  the runtime socket, on the one node that carries production. The only route to
  `netem` is node-pool cloud-init and a recycle of that single worker — a
  permanent node-level dependency, created for a test, that any OKE image change
  can silently remove again. The shaper needs no capability at all.

Rejected alternatives:

- **A compiled-in injector** (fault logic in the live server) — puts fault code
  and a selection surface in the production image; violates the same-image
  constraint outright.
- **A build-tag fault binary** — a non-shipping app variant breaks same-image
  promotion (staging would run a different binary than production).
- **toxiproxy** — TCP-only, so it cannot fault the QUIC/UDP agent path, which is
  the gap that matters. The same finding is what makes an in-path forwarder the
  answer: the shaper is the UDP equivalent, written here because there is no
  off-the-shelf one. `tc`, `comcast`, Pumba and Chaos Mesh are all wrappers
  around the kernel module the node does not ship.
- **A privileged node agent for network faults** — rejected on the measurement
  above: it would cover less than the shaper does while placing a capability-rich
  daemon on the node production runs on.
- **A separate Always-Free OKE cluster for the deployed drills** — infeasible. The 200 GB
  block-volume cap is already consumed by four 50 GB minimum-size volumes (prod
  Postgres, VictoriaMetrics, Loki, node boot); a second cluster's mandatory
  ≥50 GB boot volume exceeds the cap (compute has headroom, storage is the wall).
  See [ADR-035](./ADR-035-oke-free-tier-block-volume-remediation.md).

**No deployed fault holds any privilege.** The shaper is an ordinary
unprivileged pod running as a non-root user with every capability dropped, and
the runner scripts use nothing but the cluster's own API. Because a second
cluster is infeasible, everything runs on the one shared worker, and what keeps
that safe is that there is nothing privileged to schedule there: the guardrails
are namespace scoping (every runner refuses any namespace but
`opengate-staging`), the namespace lease each drill takes before touching
anything, bounded scenarios, and a teardown verified on every path that asserts
no pod the drill created remains.

**The machine-facing network path is in scope** — outage, one-way packet loss,
fixed delay and re-addressing, run nightly against a real agent and a simulated
fleet at once. Corruption and reordering are deliberately excluded: the protocol
seals each packet, so a damaged one fails its integrity check and is discarded —
indistinguishable from one that never arrived — and out-of-order delivery is
something the protocol resolves by design.

**Breaking the link on the server's own side is out of scope.** The shaper sits
between the machine and the server, so it fails the machine's path and not the
server's own interface. Doing that would need either a privileged node agent or
a second cluster, both of which this decision rules out.

## Consequences

- The production image contains zero fault code by construction; the absence of a
  compiled-in injector is asserted structurally (a no-import architecture rule in
  the fault suite, and a `go list -deps` assertion that the server never depends
  on the link shaper), so there is no disabled-overhead benchmark to run.
- The impairments are pure functions over datagrams with an injected generator
  and clock, so every one is asserted by an ordinary Go test with no cluster and
  no sleeping — and the generator's seed is recorded per run, which makes two
  nights comparable in a way the kernel's own emulator could not have.
- The shaper is one extra hop, about a fifth of a millisecond inside the
  cluster, and it does not carry the network layer's congestion marking through.
  It is an instrument for recovery behaviour, not for congestion-control
  research.
- Deployed faults are **coarse** — per pod / netns / ingress host, not
  per-request. Per-request in-deployment isolation is parked; the Go harness
  proves isolation per-test instead.
- The gating surfaces are the Go fault suite in normal CI and the Kubernetes and
  ingress edge drills as a required post-E2E staging stage, with no
  clean-run-history waiting period. Their determinism is therefore mandatory — a
  flaky drill blocks deploys.
- The network drill is deliberately **not** among them. It runs as its own
  nightly, reports and trends, and never blocks a release: its readings are taken
  on a shared two-processor node where a slow recovery is a trend to read rather
  than a reason to stop shipping. A scenario that could not observe the system
  emits no measurement at all, so an aborted night cannot flatter the next one.
- A drill that fails to remove what it created is a release-blocking condition
  caught by the `always()` teardown check.

The full fault catalog, harness action set, the link shaper's impairments and
guardrails, per-scenario outcomes, and recovery-SLO budgets are specified in
[Fault Injection](../infrastructure/Fault-Injection.md).
