# Phase 13b PR-C — RedisRegistry + cross-server proxy

**Created:** 2026-06-02 · **Parent:** [phase-13b-multiserver-scaling.md](phase-13b-multiserver-scaling.md) §4 PR-C · **Status:** In progress

## Context

PR-A put the `SessionRegistry` port ([registry.go](../../server/internal/relay/registry.go)) on the live relay path via the in-process adapter; PR-B landed the k8s substrate. PR-C is the ADR-023 core: a **Redis-backed `SessionRegistry`** + the **cross-server WebSocket proxy** so an agent on server A and a browser on server B relay through the affinity owner. Backend is a config swap (`REGISTRY_BACKEND=redis|inprocess`), per ADR-023 §"affinity routing".

The contract `RedisRegistry` must satisfy is the existing 6-method [`SessionRegistry`](../../server/internal/relay/registry.go) (ClaimAffinity/LookupOwner/SaveSession/DeleteSession/SubscribeEvents/PublishEvent); [`InProcessRegistry`](../../server/internal/relay/inprocess_registry.go) is the reference. The relay already expresses lifecycle through the port ([relay.go:143-166](../../server/internal/relay/relay.go#L143) claim/save/publish, [:239-242](../../server/internal/relay/relay.go#L239) delete); the cross-server hook point is [relay.go:150-156](../../server/internal/relay/relay.go#L150) (`owner != r.serverID`).

## Decisions

- **Client:** `github.com/redis/go-redis/v9` (maintained, context-first).
- **Deterministic tests:** `github.com/alicebob/miniredis/v2` (pure-Go in-memory Redis) for the adapter unit tests — always runs, no Docker, satisfies the test-determinism rule (no `t.Skip`). A real-Redis integration test mirrors [`testpg`](../../server/internal/testpg/testpg.go) via testcontainers (`testredis` leaf pkg) for behaviors miniredis approximates (Pub/Sub timing).
- **Keys:** `affinity:{token}` = owning serverID (`SET NX EX ttl`); `meta:{token}` = JSON `SessionMeta`; `events` Pub/Sub channel = JSON `SessionEvent`.
- **HA:** Redis Sentinel from the start (§6.4) — `go-redis` `NewFailoverClient`; degraded-mode posture per ADR-023 (Redis <30s → in-flight continue, new sessions fail closed; >30s → Telegram alert).

## Slices (each TDD, gauntlet-gated)

### C1 — `RedisRegistry` adapter + backend selection  ← current
- New `server/internal/relay/redis_registry.go` implementing `SessionRegistry` on go-redis. `redis_registry_test.go` first (miniredis): claim-wins/claim-loses, TTL expiry reclaim, LookupOwner not-found, SaveSession idempotent, Delete, Publish→Subscribe round-trip + fan-out + ctx-cancel close, invalid-arg guards. Mirror the InProcessRegistry test matrix exactly so both adapters prove the same contract.
- `main.go` selects adapter by `REGISTRY_BACKEND` (default `inprocess`); `REDIS_ADDR`/`REDIS_SENTINEL_ADDRS`/`REDIS_MASTER_NAME` config. `WithRegistry` unchanged.
- Redis to the chart (`deploy/helm/opengate`): Sentinel StatefulSet + Service (gated `redis.enabled`), `REGISTRY_BACKEND=redis` wired in values; `make lint-k8s` stays green.
- **ADR-031** (supersedes ADR-023's Redis-HA deferral) + decisions row + techdebt (Medium: new Redis operational surface — AOF+RDB backups, pooling) per plan §7.

### C2 — cross-server WebSocket proxy
- At [relay.go:150-156](../../server/internal/relay/relay.go#L150), when `owner != serverID`, dial the owner's internal relay endpoint, set `X-OpenGate-Proxy: {serverID}`, and splice frames in the same `relay.Conn` wire format. Loop-guard via the header. Affinity routing keeps same-server sessions zero-hop.

### C3 — `/healthz` Redis + readiness
- `/healthz` reports Redis status; k8s readinessProbe drains the pod on Redis loss; degraded-mode + Telegram alert wiring (ADR-023 §"Recovery posture", plan §7).

## Out of scope (later)
- `make e2e-multiserver` + load baseline (**PR-D**); HPA / scale-out (**PR-E**).

## Verification
- Unit: `go test ./internal/relay/...` (miniredis, always-run). Integration: testcontainers Redis. Full `/precommit` per commit. Coverage ≥ thresholds.
