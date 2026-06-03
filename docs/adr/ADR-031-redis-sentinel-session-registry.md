# ADR-031: Redis Sentinel-backed distributed SessionRegistry

**Status:** Accepted
**Date:** 2026-06-03
**Supersedes:** ADR-023's Redis-HA deferral ("introduce the registry now, pick the
concrete tech [and HA] at Phase 13b kickoff"). ADR-023 remains the spine — the
`SessionRegistry` port, affinity-TTL model, cross-server-proxy shape, and
degraded-mode posture are unchanged; this ADR picks the concrete backend and
commits to its HA topology. ADR-030 explicitly deferred Redis to this ADR.

## Context

PR-A put the [`SessionRegistry`](../../server/internal/relay/registry.go) port on
the live relay path via the in-process adapter; PR-B landed the OKE/Helm
substrate (ADR-030). PR-C is the ADR-023 core: a Redis-backed `SessionRegistry`
so an agent on server A and a browser on server B relay through the session's
affinity owner. The relay code already expresses lifecycle through the port, so
the multiserver story is a **backend swap**, not a relay rewrite.

This ADR records the **C1** decisions (the adapter, backend selection, and the
chart's Redis topology). The cross-server WebSocket proxy (C2) and the
`/healthz`-Redis + readiness + degraded-mode wiring (C3) are forthcoming under
the same PR-C; their concrete shape is already fixed by ADR-023 and is not
re-litigated here.

## Decision

1. **Backend tech: `redis/go-redis/v9`.** Context-first, maintained, and its
   `UniversalClient`/`NewFailoverClient` abstract single-instance vs Sentinel
   behind one type — so the adapter is HA-agnostic. The
   [`RedisRegistry`](../../server/internal/relay/redis_registry.go) implements
   all six `SessionRegistry` methods; `InProcessRegistry` stays the reference
   for the contract.

2. **Key schema** (prefix `opengate:relay:`):
   - `affinity:{token}` — owning serverID, `SET … EX <ttl>`. The claim is an
     atomic **claim-or-get Lua script** (`GET`; return current owner if set,
     else `SET EX` and return self), removing the SETNX-then-GET race.
   - `meta:{token}` — JSON `SessionMeta`, with a generous backstop TTL so a
     crashed owner's metadata cannot leak forever.
   - `events` — JSON `SessionEvent` over Redis Pub/Sub (fan-out to peer servers;
     `PublishEvent` is a no-op when no server subscribes).

3. **Backend selection: `REGISTRY_BACKEND`** = `inprocess` (default) | `redis`,
   resolved by [`relay.SessionRegistryFromConfig`](../../server/internal/relay/backend.go)
   — a tested seam returning the adapter plus an `io.Closer` for shutdown.
   Redis connection config: `REDIS_ADDR` (single instance) **or**
   `REDIS_SENTINEL_ADDRS` + `REDIS_MASTER_NAME` (failover), plus optional
   `REDIS_PASSWORD`. `WithRegistry` on the relay is unchanged.

4. **HA: Redis Sentinel from the start.** The Helm chart ships a Redis data
   StatefulSet (pod-0 is the bootstrap master; replicas `replicaof` it; on
   restart **every** node rediscovers the live master from Sentinel before
   choosing master/replica role) and a Sentinel StatefulSet (quorum 2) with
   headless services. `go-redis`'s failover client is handed every Sentinel
   pod's stable FQDN. The whole topology is gated `redis.enabled` and is
   **dormant by default** (`REGISTRY_BACKEND` stays `inprocess`) until the
   multi-replica cutover — but it renders under `make lint-k8s` so the manifests
   are schema- and policy-validated now.

5. **Deterministic tests.** Adapter unit tests run against
   `alicebob/miniredis/v2` (pure-Go, in-process) so they always run with no
   Docker and no skips, per ADR-029. A real-Redis integration test (testcontainers,
   mirroring [`testpg`](../../server/internal/testpg/testpg.go)) covers the
   behaviours miniredis only approximates (Pub/Sub and Sentinel timing) and
   lands with C2/C3.

## Consequences

- **New operational surface (techdebt, Medium):** a Redis Sentinel cluster —
  AOF+RDB persistence, Sentinel quorum, connection pooling, and backups — none
  of which the in-process adapter needed. Recorded in `.claude/techdebt.md`.
- **Dormant-but-unexercised HA:** the Sentinel topology passes lint/policy but is
  not runtime-tested while `redis.enabled` is false. Master rediscovery on
  restart, failover, and replica re-pointing need a live integration check
  before the multi-replica cutover flips the backend. Tracked in techdebt.
- **miniredis ≠ Redis:** Lua semantics and Pub/Sub fan-out are approximated; the
  testcontainers integration test is the authority for those paths.
- **Affinity correctness depends on the proxy (C2):** until the cross-server
  WebSocket proxy lands, `REGISTRY_BACKEND=redis` records ownership correctly
  but a cross-server session pair is not yet spliced — so the backend must stay
  `inprocess` in production until C2/C3 complete. This ADR does not enable Redis
  in any environment overlay.
