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

**Peer addressing (decided 2026-06-03):** `serverID` = **Pod IP** via the k8s Downward API (`OPENGATE_SERVER_ID` ← `status.podIP` in the Deployment). A peer is dialed **directly** at `http://{owner}:{internalPort}/internal/relay/{token}?side={side}` over the flat cluster overlay — no headless Service, no DNS (pod IPs are routable container-to-container). Pod IPs churn on restart, which the affinity TTL already tolerates. Same `internalPort` cluster-wide (homogeneous pods), so a dialer knows the peer's port = its own.

**Internal relay endpoint.** A **separate internal HTTP listener** (`-internal-listen`, default `:9091`; `OPENGATE_INTERNAL_LISTEN`), distinct from the public `:8080` and **never** behind the ingress. Route `GET /internal/relay/{token}?side={agent|browser}`:
- Requires header `X-OpenGate-Proxy: {callerServerID}` (loop-guard + identity) and, when `OPENGATE_PROXY_SECRET` is set, `X-OpenGate-Proxy-Secret` must match (defense-in-depth on top of network isolation; ADR-023 §"cross-server auth" = network boundary, this adds a cheap shared secret).
- Accepts the WS, wraps in the existing `WSConn`, and registers it into the **local** relay via a new `RegisterLocal` path that **skips the affinity/proxy decision** — a proxied conn never re-proxies (the loop guard). The owner then pipes agent↔proxied-side with the unchanged `pipe()`.

**Relay proxy splice.** New injectable port `PeerDialer` (`Dial(ctx, owner, token, side) (Conn, error)`), wired via `WithPeerDialer`; production adapter dials the URL above with the headers and returns a `WSConn`. At [relay.go:150](../../server/internal/relay/relay.go#L150), when `ClaimAffinity` returns `owner != serverID` and a dialer is set:
- Do **not** pair locally. Store a `proxied` session entry (so `WaitForPeer` unblocks once the peer dial succeeds), skip the owner-only registry writes (`SaveSession`/`EventCreated`), then bidirectionally `copyMessages(local, peer)`; close both + delete the entry on either end. Dial failure → close the local conn (both sides reconnect with a fresh token per ADR-023). Same-server sessions stay zero-hop (owner == serverID → existing path untouched).

**Tests (TDD, no Docker).** Relay: fake `PeerDialer` returning an in-memory `Conn` pair proves owner!=self → splice both directions, dial-failure closes the conn, owner==self stays local (zero dial). Internal endpoint: `httptest` server + real `WSConn` proves header-required, secret-required-when-set, side-parse, and register-local-skips-proxy (loop guard). Mirror the existing relay test style.

**Config/Helm.** `main.go`: start the internal listener; build the production `PeerDialer`; `OPENGATE_INTERNAL_LISTEN`/`OPENGATE_PROXY_SECRET`. Helm `server-deployment.yaml`: `OPENGATE_SERVER_ID` ← `fieldRef: status.podIP`, internal containerPort, `OPENGATE_PROXY_SECRET` from the existing secret; `make lint-k8s` green. **ADR-033** (cross-server proxy wire protocol + pod-IP addressing; the detail ADR-023 deferred) + decisions row.

### C3 — `/healthz` Redis + readiness
- `/healthz` reports Redis status; k8s readinessProbe drains the pod on Redis loss; degraded-mode + Telegram alert wiring (ADR-023 §"Recovery posture", plan §7).

## Out of scope (later)
- `make e2e-multiserver` + load baseline (**PR-D**); HPA / scale-out (**PR-E**).

## Verification
- Unit: `go test ./internal/relay/...` (miniredis, always-run). Integration: testcontainers Redis. Full `/precommit` per commit. Coverage ≥ thresholds.
