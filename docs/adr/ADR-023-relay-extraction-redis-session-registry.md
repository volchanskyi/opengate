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

The plan's earlier "extractable to a separate binary without code changes in `api`" criterion was impossible against this code — a distributed session registry is the missing primitive. Resolved ([`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/archive/modular-monolith-evaluation.md) §4.5, R2 Q4): introduce the registry now, and pick the concrete tech.

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

Resolved (R2 Q4). Redis was chosen over Postgres `LISTEN/NOTIFY` and over an in-process gossip protocol for the following reasons:

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

## Amendments

The `SessionRegistry` port, affinity-TTL model, cross-server-proxy shape, and
degraded-mode posture above are the spine and are unchanged; these amendments
pick the concrete backend and record the proxy's wire detail that this ADR
deferred to implementation. (Formerly standalone ADR-031 and ADR-033,
consolidated here when per-file ADRs became mutable —
[ADR-036](ADR-036-mutable-adrs-current-state-doctrine.md).)

### Amendment 1 — Redis Sentinel-backed registry, the concrete backend (2026-06-03)

Resolves the "pick the concrete tech [and HA] at Phase 13b kickoff" deferral
(C1). [ADR-030](ADR-030-kubernetes-adoption-oke-helm.md) deferred Redis to here.

1. **Backend tech: `redis/go-redis/v9`.** Context-first and maintained; its
   `UniversalClient`/`NewFailoverClient` abstract single-instance vs Sentinel
   behind one type, so the adapter is HA-agnostic. The
   `RedisRegistry` implements
   all six `SessionRegistry` methods; `InProcessRegistry` stays the contract
   reference.
2. **Key schema** (prefix `opengate:relay:`): `affinity:{token}` = owning
   serverID via `SET … EX <ttl>`, claimed by an atomic **claim-or-get Lua
   script** (`GET`; return current owner if set, else `SET EX` and return self)
   that removes the SETNX-then-GET race; `meta:{token}` = JSON `SessionMeta` with
   a generous backstop TTL so a crashed owner's metadata cannot leak forever;
   `events` = JSON `SessionEvent` over Pub/Sub (`PublishEvent` is a no-op when no
   server subscribes).
3. **Backend selection: `REGISTRY_BACKEND`** = `inprocess` (default) | `redis`,
   resolved by `relay.SessionRegistryFromConfig`
   (a tested seam returning the adapter plus an `io.Closer`). Redis config:
   `REDIS_ADDR` (single) **or** `REDIS_SENTINEL_ADDRS` + `REDIS_MASTER_NAME`
   (failover), plus optional `REDIS_PASSWORD`.
4. **HA: Redis Sentinel from the start.** The Helm chart ships a Redis data
   StatefulSet (pod-0 bootstrap master; replicas `replicaof` it; on restart every
   node rediscovers the live master from Sentinel before choosing its role) and a
   Sentinel StatefulSet (quorum 2) with headless services. The topology is gated
   `redis.enabled` and **dormant by default** (`REGISTRY_BACKEND` stays
   `inprocess`) until the multi-replica cutover, but renders under `make lint-k8s`
   so the manifests are schema- and policy-validated now.
5. **Deterministic tests.** Adapter unit tests run against
   `alicebob/miniredis/v2` (pure-Go, in-process) so they always run with no Docker
   and no skips ([ADR-029](ADR-029-test-determinism-no-silent-skips.md)). A
   real-Redis testcontainers test (mirroring
   [`testpg`](../../server/internal/testpg/testpg.go)) covers what miniredis only
   approximates (Pub/Sub and Sentinel timing) and lands with the proxy work.

Consequences: a new Redis Sentinel operational surface (AOF+RDB, quorum, pooling,
backups) recorded as Medium techdebt; a **dormant-but-unexercised HA** topology
that passes lint/policy but needs a live failover check before the cutover; and
the fact that ownership is recorded correctly but a cross-server pair is not
spliced until Amendment 2 — so the backend stays `inprocess` in production until
then, and Redis is enabled in no overlay here.

### Amendment 2 — cross-server proxy: pod-IP addressing + internal listener (2026-06-04)

Records the data path (C2) the spine deferred to implementation. Recording
ownership is inert without it; this is that path.

1. **Peer addressing = pod IP via the Downward API; direct dial, no DNS.**
   `serverID` is the pod IP (`OPENGATE_SERVER_ID` ← `status.podIP`). A peer is
   dialed **directly** at `ws://{owner}:{internalPort}/internal/relay/{token}?side={side}`
   over the flat cluster overlay — pod IPs are routable container-to-container, so
   no headless Service and no DNS are needed; homogeneous pods share the dialer's
   own `internalPort`. Pod-IP churn on restart is absorbed by the affinity TTL.
2. **A separate internal HTTP listener**
   ([`-internal-listen`](../../server/cmd/meshserver/main.go), default `:9091`;
   `OPENGATE_INTERNAL_LISTEN`), distinct from the public `:8080` and never fronted
   by the public router or ingress. One route:
   `GET /internal/relay/{token}`.
3. **Tunnel auth = network boundary + loop-guard header + optional secret.**
   Every request carries `X-OpenGate-Proxy: {callerServerID}` (identity **and**
   the loop guard — only a peer relay sets it); when `OPENGATE_PROXY_SECRET` is
   configured, `X-OpenGate-Proxy-Secret` must match via constant-time compare.
   All checks run **before** the WebSocket upgrade, so a rejected peer gets a
   plain HTTP status, not a half-open socket. The full token rides the URL path
   (private overlay only); only the redacted prefix is logged
   ([ADR-027](ADR-027-adversarial-pentest-precommit-gate.md)).
4. **`PeerDialer` port + loop guard.** A new injectable
   [`PeerDialer`](../../server/internal/relay/relay.go) (`WithPeerDialer`) is
   consulted only when the registry reports a foreign owner. The owner accepts the
   tunnel via a dedicated `RegisterLocal` path that makes **no** affinity/proxy
   decision, so a proxied conn never re-proxies (the loop guard). Same-server
   sessions stay zero-hop.
5. **Synchronous dial, fail-fast, bounded half-open.** The non-owner dials the
   peer **inside** `Register` (outside the session lock); the sole caller blocks
   immediately after, so the synchronous dial costs no latency and gives the
   cleanest error path — a dial failure closes the local conn and drops the
   session (fail-fast, no retry state). On the owner, `RegisterLocal` waits up to
   `affinityTTL` for the local peer, then tears down a half-open proxied conn
   rather than parking it forever.
6. **Deterministic tests, no Docker.** The splice is proven with a fake
   `PeerDialer` over in-memory conn pairs; the internal endpoint and production
   `HTTPPeerDialer` with `httptest` + real WebSocket dials — all always-run
   ([ADR-029](ADR-029-test-determinism-no-silent-skips.md)).

Consequences: the data path completes the core (an A-agent / B-browser pair
relays through A), but `REGISTRY_BACKEND=redis` is still enabled in no overlay —
C3 (`/healthz`-Redis, readiness drain, degraded-mode) gates the production
cutover. The new private `:9091` listener has **no NetworkPolicy** restricting it
to peer pods — follow-up hardening tracked in
[`.claude/techdebt.md`](../../.claude/techdebt.md). Direct pod-IP dialing assumes
a k8s flat overlay; in single-server / docker-compose dev the owner is always
self, so the proxy path is inert.

## References

- Plan: [`.claude/plans/modular-monolith-evaluation.md`](../../.claude/plans/archive/modular-monolith-evaluation.md) §4.5 (registry decision), §6 (Redis-as-new-surface pitfall), §7 (extractability verification)
- Upstream: [ADR-020](ADR-020-modular-monolith-full-hexagonal.md) — modular-monolith scope and style
- Phase context: [`.claude/phases.md`](../../.claude/phases.md) — Phase 13b entry
- Operational simplicity precedent: [ADR-018](ADR-018-oci-bastion-operator-access.md)
- Pinch-point: [`server/internal/relay/relay.go:35-60`](../../server/internal/relay/relay.go#L35-L60)
- Alert scoping reference: terraform-drift pattern at commit `678dda3`
