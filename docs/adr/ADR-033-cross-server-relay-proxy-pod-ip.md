# ADR-033: Cross-server relay proxy — pod-IP addressing and internal listener

**Status:** Accepted
**Date:** 2026-06-04
**Extends:** [ADR-023](ADR-023-relay-extraction-redis-session-registry.md) (the
`SessionRegistry` port, affinity-TTL model, and the cross-server-proxy *shape*)
and [ADR-031](ADR-031-redis-sentinel-session-registry.md) (the concrete Redis
backend that makes ownership cross-server). ADR-023 deferred the proxy's wire
detail and peer addressing to implementation; this ADR records them. Nothing in
ADR-023/031 is changed.

## Context

[ADR-031](ADR-031-redis-sentinel-session-registry.md) (PR-C C1) made the
[`SessionRegistry`](../../server/internal/relay/registry.go) record session
ownership across servers: when an agent on server A and a browser on server B
hit different pods, `ClaimAffinity` tells B that A owns the token. But recording
ownership is inert without a data path — C1 noted that a cross-server pair is
*not yet spliced* and that `REGISTRY_BACKEND=redis` must stay dormant until this
proxy lands. C2 is that data path.

The relay already drives lifecycle through the port, and the splice seam already
exists: [`Register`](../../server/internal/relay/relay.go) computes
`owner != serverID` on the first side. Two questions were open: **how a server
addresses the owning peer**, and **the wire contract of the tunnel**.

## Decision

1. **Peer addressing = pod IP via the Downward API; direct dial, no DNS.**
   `serverID` is the pod IP (`OPENGATE_SERVER_ID` ← `status.podIP` in the
   Deployment). A peer is dialed **directly** at
   `ws://{owner}:{internalPort}/internal/relay/{token}?side={side}` over the flat
   cluster overlay — pod IPs are routable container-to-container, so no headless
   Service and no DNS are needed. Pods are homogeneous, so every peer shares the
   dialer's own `internalPort`. Pod IPs churn on restart, which the affinity TTL
   already tolerates (a stale owner's claim expires and is re-won).

2. **A separate internal HTTP listener**
   ([`-internal-listen`](../../server/cmd/meshserver/main.go), default `:9091`;
   `OPENGATE_INTERNAL_LISTEN`), distinct from the public `:8080` and **never**
   fronted by the public router or the ingress. It serves exactly one route,
   [`GET /internal/relay/{token}`](../../server/internal/api/internal_relay.go).

3. **Tunnel auth = network boundary + loop-guard header + optional secret.**
   ADR-023's "cross-server auth = network boundary" stands; this adds cheap
   defense-in-depth. Every request must carry `X-OpenGate-Proxy: {callerServerID}`
   (identity **and** the loop guard — only a peer relay sets it). When
   `OPENGATE_PROXY_SECRET` is configured, `X-OpenGate-Proxy-Secret` must match it
   via a constant-time compare. All three checks run **before** the WebSocket
   upgrade, so a rejected peer gets a plain HTTP status, not a half-open socket.
   The full token rides the URL path (private overlay only); only the redacted
   prefix is ever logged (ADR-027).

4. **`PeerDialer` port + the loop guard.** A new injectable
   [`PeerDialer`](../../server/internal/relay/relay.go) (`WithPeerDialer`) is
   consulted only when the registry reports a foreign owner. The owner accepts
   the tunnel and registers it through a dedicated
   [`RegisterLocal`](../../server/internal/relay/relay.go) path that makes **no**
   affinity or proxy decision — a proxied conn therefore never re-proxies (the
   loop guard). Same-server sessions stay zero-hop (`owner == serverID` → the
   existing local pipe, no dial).

5. **Synchronous dial, fail-fast, bounded half-open.** The non-owner dials the
   peer **inside** `Register` (outside the session lock); the sole caller blocks
   immediately afterward anyway, so a synchronous dial costs no latency and gives
   the cleanest error path: a dial failure closes the local conn and drops the
   session so the client reconnects with a fresh token (ADR-023 fail-fast — no
   retry state in the relay). On the owner, `RegisterLocal` waits up to
   `affinityTTL` for the local peer; if a stale affinity claim points at a server
   whose side is already gone, the half-open proxied conn is torn down rather
   than parked forever.

6. **Deterministic tests, no Docker.** The relay splice is proven with a fake
   `PeerDialer` over in-memory conn pairs; the internal endpoint and the
   production `HTTPPeerDialer` are proven with `httptest` + real WebSocket dials
   (header-required, secret-required-when-set, side-parse, register-local pairs,
   half-open timeout, dialer round-trip). All always-run, per ADR-029.

## Consequences

- **The data path completes PR-C's core:** with the proxy in place, an A-agent /
  B-browser pair relays through A. `REGISTRY_BACKEND=redis` is still **not**
  enabled in any overlay here — C3 (`/healthz`-Redis, readiness drain,
  degraded-mode) gates the production cutover.
- **New private listener per pod (`:9091`):** declared as a pod-to-pod
  containerPort under `redis.enabled`, never exposed via Service/ingress.
  A NetworkPolicy restricting it to peer pods is follow-up hardening
  (tracked in `.claude/techdebt.md`), not part of this ADR.
- **Addressing is k8s-flat-overlay-specific:** direct pod-IP dialing assumes
  routable pod IPs and homogeneous ports. In single-server / docker-compose dev
  the owner is always self, so the proxy path is inert — no behavior change.
- **Secret is optional:** with `proxySecretEnabled=false` the tunnel relies on
  the network boundary alone, matching ADR-023's baseline; enabling it requires
  an `OPENGATE_PROXY_SECRET` key in the server secret.
