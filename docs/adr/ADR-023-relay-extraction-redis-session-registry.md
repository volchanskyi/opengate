# ADR-023: Relay extraction readiness — Redis-backed distributed session registry

Date: 2026-05-19
Status: Accepted

## Context

Phase 13b (Multiserver & Scaling) is High-priority and pending per [`.claude/phases.md`](../../.claude/phases.md). Its core requirements are cross-server routing, a relay pool, and Kubernetes deployment. The relay component is the natural process-boundary candidate.

Current state ([`server/internal/relay/relay.go:45-60`](../../server/internal/relay/relay.go#L45-L60)):

- `Relay` holds an in-memory `sync.Map[SessionToken]*session`.
- Each `session` carries two `Conn` interfaces (one agent, one browser side).
- State is purely per-connection and ephemeral — no database backing.
- `relay.Conn` ([`relay.go:35-43`](../../server/internal/relay/relay.go#L35-L43)) is already a real port with the `WSConn` adapter as the production implementation.

The plan's earlier "extractable to a separate binary without code changes in `api`" criterion was impossible against this code — a distributed session registry is the missing primitive. Resolved 2026-05-19 ([`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/modular-monolith-evaluation.md) §4.5, R2 Q4): introduce the registry now, and pick the concrete tech.

## Decision

### `SessionRegistry` outbound port

Introduce a new outbound port owned jointly by the `session` and `relay` modules:

```go
type SessionRegistry interface {
    // ClaimAffinity atomically claims ownership of a session token for this
    // server. Returns the owning serverID (this server if the claim succeeded,
    // another server if a prior claim is still alive).
    ClaimAffinity(ctx context.Context, token SessionToken, serverID string, ttl time.Duration) (owner string, err error)

    // LookupOwner returns the serverID that currently owns the session.
    LookupOwner(ctx context.Context, token SessionToken) (string, error)

    // SaveSession persists session metadata (created-at, expected-sides, etc.)
    // — not the live Conn pairs, which stay in-process on the owning server.
    SaveSession(ctx context.Context, token SessionToken, meta SessionMeta) error

    // DeleteSession releases the affinity claim and removes metadata.
    DeleteSession(ctx context.Context, token SessionToken) error

    // SubscribeEvents returns a channel of session-lifecycle events for
    // sessions this server owns or is subscribed to. Used to wake the
    // remote side when the agent side disconnects on another server.
    SubscribeEvents(ctx context.Context) (<-chan SessionEvent, error)

    // PublishEvent broadcasts a session-lifecycle event to subscribers.
    PublishEvent(ctx context.Context, evt SessionEvent) error
}
```

The port is satisfied by two adapters:

1. **`InProcessRegistry`** — wraps the current `sync.Map`. Used for single-server deployments (today). `ClaimAffinity` always succeeds for the calling server. `Publish/Subscribe` is in-process channel routing.
2. **`RedisRegistry`** — Phase 13b adapter. Implementation details below.

The relay code does not know which adapter is in use — phase 13b becomes a config swap (`REGISTRY_BACKEND=redis`), not a code change.

### Storage tech: Redis

Resolved 2026-05-19 (R2 Q4). Redis was chosen over Postgres `LISTEN/NOTIFY` and over an in-process gossip protocol for the following reasons:

- Industry-standard for relay/session registries; well-understood failure modes.
- Atomic `SETNX` semantics map directly to `ClaimAffinity`.
- Native Pub/Sub for `Publish/Subscribe`.
- Lower latency than Postgres `LISTEN/NOTIFY` at the projected Phase 13b session counts.
- Always-Free OCI deployment fits the budget constraint that drove [ADR-018](ADR-018-oci-bastion-operator-access.md).

### Affinity routing scheme

When an agent connects to server A and a browser connects to server B for the same session token:

1. Both servers call `ClaimAffinity(token, ownServerID, TTL)`. The first call wins; the second call returns the winner's serverID.
2. The losing server proxies its connection's frames to the owning server via an internal cross-server WebSocket. The owning server holds the `Conn` pair locally and pipes them as it does today.
3. When either side disconnects, the owning server `Publish`es a `SessionEvent`; the proxy server receives it via `Subscribe` and tears down its proxy.
4. TTL on the affinity claim covers the case where the owning server dies mid-session; the proxy server reclaims after expiry.

The cross-server WebSocket protocol is an internal extension of the existing `relay.Conn` adapter — same wire format, additional `X-OpenGate-Proxy: serverID` header. Specified in detail in the relay-extraction PR; not in this ADR.

### Recovery posture

- **Redis unavailable for < 30s**: relay continues serving in-flight sessions; new sessions fail open (rejected) rather than fail closed (silent split-brain). Health check at [`/healthz`](../../server/internal/api/api.go) reports Redis status; load balancer drains accordingly.
- **Redis unavailable for > 30s**: server enters degraded mode — refuses new session registrations, continues servicing existing ones until they drain. Operational alert via existing Telegram channel (consistent with terraform-drift alert pattern at commit `678dda3`).
- **Owning server crash mid-session**: TTL on the affinity claim guarantees the session is reclaimable by another server within `TTL` seconds. Default TTL = 30s; reclaim event triggers session-state rebuild from `SaveSession` metadata. The current session pair is lost; both sides must reconnect with a fresh token (existing behavior on agent disconnect — reused).

### Phase 13b integration test

A new `make e2e-multiserver` target boots two server containers, one Redis container, and one shared Postgres container. Tests verify:

1. Agent connects to server A; browser connects to server B; frames flow.
2. Owning server is killed mid-session; reclaim within TTL window; both sides reconnect successfully.
3. Redis killed mid-session; in-flight sessions drain; new sessions rejected; alert fires.

Lives in [`tests/e2e-multiserver/`](../../tests/) (new). Wired into the existing E2E gauntlet step ([`scripts/precommit-gauntlet.sh`](../../scripts/precommit-gauntlet.sh)) gated by a `OPENGATE_MULTISERVER_E2E=1` env var so it does not slow the default pre-commit run.

### Memberlist deferred

A gossip layer (HashiCorp's `memberlist` / SWIM) was considered alongside Redis. **Rejected for the initial Phase 13b cut.** Rationale:

- Kubernetes provides DNS-based service discovery and readiness probes natively — memberlist re-implements those for the small server count (2–5) at Phase 13b launch.
- Adds a second coordination mental model and a UDP port to the security surface.
- The combo's value materializes above ~20 servers OR when Redis Pub/Sub fanout becomes the hot path. Neither applies at 13b launch.

Memberlist becomes warranted IF **(a)** server count grows past ~20 nodes, OR **(b)** Redis Pub/Sub fanout becomes the hot path for session events, OR **(c)** the deployment moves outside Kubernetes. At that point a successor ADR introduces the gossip layer alongside the Redis registry.

### Migration triggers

- ADR acceptance → introduce `SessionRegistry` port + `InProcessRegistry` adapter. No behavior change.
- Phase 13b kickoff → introduce `RedisRegistry` adapter; wire the cross-server proxy; ship `make e2e-multiserver`; run baseline load test.

## Out of scope

- **Redis HA / sentinel / cluster topology decisions** — deferred until Phase 13b kickoff when load characteristics are clearer.
- **Memberlist / gossip layer** — see deferral above.
- **Cross-server agent migration** (live re-routing an agent from server A to server B mid-session) — not in 13b scope; agents reconnect with a fresh token if their server dies.
- **Geographic / multi-region relay pool** — single-region only at Phase 13b launch.
- **Authentication of cross-server proxy traffic beyond mTLS** — same Caddy + ACME pattern as [ADR-004](../Architecture-Decision-Records.md) covers; no new auth scheme.

## Consequences

**Positive.**

- `relay` becomes extractable as a separate binary. Single deployment becomes the no-op case (`InProcessRegistry`); multi-deployment is a config swap to `RedisRegistry`.
- The `SessionRegistry` port is a hexagonal seam usable beyond relay (e.g. future session-recording metadata, audit cross-references).
- Phase 13b extraction work scopes to one PR (Redis adapter + cross-server proxy + multiserver E2E), not a redesign.

**Accepted trade-offs.**

- Redis is a new operational surface: backups (AOF + RDB), connection pooling, eventual sentinel/cluster decisions. Recorded as a Medium tech-debt item in [`.claude/techdebt.md`](../../.claude/techdebt.md) once this ADR ships.
- Phase 13b becomes dependent on Redis being healthy. Mitigated by the degraded-mode recovery posture and the TTL-based reclaim.
- Cross-server proxy adds a hop to sessions where agent and browser hit different servers. Latency cost is one intra-VCN hop — acceptable.
- The 30s TTL is a tunable that affects reclaim-after-crash latency. Default chosen for clarity; revisit after first 13b load test.

## References

- Plan: [`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/modular-monolith-evaluation.md) §4.5 (registry decision), §6 (Redis-as-new-surface pitfall), §7 (extractability verification)
- Upstream: [ADR-020](ADR-020-modular-monolith-full-hexagonal.md) — modular-monolith scope and style
- Phase context: [`.claude/phases.md`](../../.claude/phases.md) — Phase 13b entry
- Operational simplicity precedent: [ADR-018](ADR-018-oci-bastion-operator-access.md)
- Pinch-point: [`server/internal/relay/relay.go:35-60`](../../server/internal/relay/relay.go#L35-L60)
- Alert scoping reference: terraform-drift pattern at commit `678dda3`
