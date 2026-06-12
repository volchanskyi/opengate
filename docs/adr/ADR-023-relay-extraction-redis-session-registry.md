# ADR-023: Relay session-registry seam and distributed-routing design

Date: 2026-05-19
Status: Accepted

## Context

The relay pairs an agent connection and a browser connection in one process.
Live connections remain in the relay's in-memory session map. A small outbound
port records session metadata without coupling pairing behavior to a storage
implementation.

The production construction in
[`main.go`](../../server/cmd/meshserver/main.go) uses the in-process adapter.
The current interface is defined in
[`registry.go`](../../server/internal/relay/registry.go).

## Decision

### Retain a slim `SessionRegistry` port

`SessionRegistry` contains only the operations used by the single-server relay:

- `SaveSession` records token metadata when the first side arrives.
- `DeleteSession` removes the metadata after the relay finishes.
- `Ping` participates in the API readiness check.

[`InProcessRegistry`](../../server/internal/relay/inprocess_registry.go) is the
only adapter. Registry failures are logged rather than allowed to interrupt the
live connection pair, because the relay's session map remains the source of
truth.

The seam stays because metadata persistence is a coherent outbound concern and
because a future relay pool can add a distributed adapter without moving
session-lifecycle logic back into API handlers.

### Remove the distributed implementation

The Redis adapter, Sentinel topology, affinity ownership operations,
cross-server WebSocket proxy, internal listener, degraded-mode state machine,
and multiserver harness are removed.

The removed implementation was never exercised by the production topology and
introduced an operational system whose backup, failover, security, storage, and
monitoring requirements were disproportionate to a single-replica free-tier
deployment. Carrying an unproved path also made the live local-pairing behavior
harder to reason about.

[`Multiscale-Readiness.md`](../Multiscale-Readiness.md) retains the functional
and non-functional requirements for rebuilding distributed routing when demand
justifies it.

## Reverted Amendments

### Redis Sentinel registry

The removed adapter used Redis atomic ownership claims and Pub/Sub because those
primitives map naturally to session affinity and lifecycle events. A future
implementation may reuse that shape, but it must include deterministic adapter
tests, live failover drills, backup and restore, monitoring, and a storage plan
before becoming deployable.

### Cross-server relay proxy

The removed proxy routed a connection that landed on a non-owning replica to
the owning replica over an authenticated internal WebSocket. A future
implementation must preserve loop prevention, bounded half-open teardown,
pre-upgrade authentication, peer-network isolation, and end-to-end
foreign-owner relay tests.

Former standalone ADR-031 and ADR-033 are consolidated into these amendments
under [ADR-036](ADR-036-mutable-adrs-current-state-doctrine.md).

## Consequences

- The live relay is local-pairing-only and has one registry adapter.
- Readiness still checks the registry through the retained port.
- Multi-replica routing is a rebuild, not a configuration switch.
- Git history and the readiness specification preserve the evaluated Redis and
  proxy design without keeping dormant runtime code.

## References

- [ADR-020](ADR-020-modular-monolith-full-hexagonal.md) — earned-port and
  modular-monolith rules
- [`registry.go`](../../server/internal/relay/registry.go) — current port
- [`relay.go`](../../server/internal/relay/relay.go) — current pairing behavior
- [`Multiscale-Readiness.md`](../Multiscale-Readiness.md) — scale-out rebuild
  specification
- [Teardown record](../../.claude/plans/archive/dormant-scale-out-teardown.md)
