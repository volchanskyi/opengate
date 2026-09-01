---
adr: 037
title: Client-first QUIC handshake and fast-path reconnect model
status: Accepted
date: 2026-06-18
supersedes: ADR-005 (rationale only)
---

# ADR-037: Client-First QUIC Handshake and Fast-Path Reconnect Model

## Status

Accepted.

This ADR supersedes the **rationale** in [ADR-005](../Architecture-Decision-Records.md)
that treated server-opened QUIC control streams as a quic-go mTLS workaround.
This per-file ADR carries the full model; ADR-005 records the transport choice
and points here, per
[ADR-036](ADR-036-mutable-adrs-current-state-doctrine.md).

## Context

Phase 4 introduced a server-opens/server-speaks-first QUIC control stream after
a deadlock was misdiagnosed as an mTLS-specific quic-go defect. Later evidence
showed the issue was ordinary QUIC stream discovery: the endpoint that opens a
stream must write before the peer can accept and read it. The behavior
reproduces with and without mTLS and is not a quic-go defect.

The server-opened model also conflicted with the intended reconnect design. The
fast path requires the agent to initiate reconnect state by sending the first
handshake byte (`0x14`), which cannot happen when the server owns stream
creation and speaks first.

W1, W2, W3, and W5 settled the model:

- W1 realigned the control stream so the agent opens and writes first.
- W2 implemented `0x14` `SkipAuth` as an agent-initiated fast path.
- W3 measured TLS session resumption and chose 1-RTT resumption while deferring
  0-RTT early data.
- W5 retired the unused proof-message type bytes and bounded control-stream
  accept.

## Decision

### 1. The agent owns the control stream and first byte

The agent opens the bidirectional QUIC control stream and immediately writes the
first handshake message. The server accepts the stream, reads the first
handshake byte, and branches:

- `0x11` `AgentHello` — cold-start or fallback handshake. The server validates
  the advertised agent certificate hash against the mTLS peer certificate and
  replies with `0x10` `ServerHello`.
- `0x14` `SkipAuth` — reconnect fast path. The server verifies the cached CA
  certificate hash is current and sends no handshake reply.

This restores conventional client-initiated stream IDs (even stream IDs per RFC
9000 §2.1) and removes the server-opened workaround. The live code paths are
[`server/internal/agentapi/server.go`](../../server/internal/agentapi/server.go),
[`server/internal/agentapi/handshaker.go`](../../server/internal/agentapi/handshaker.go),
and [`agent/crates/mesh-agent/src/main.rs`](../../agent/crates/mesh-agent/src/main.rs).

### 2. Authentication is mTLS-only

The QUIC/TLS layer authenticates the agent with `RequireAndVerifyClientCert`,
and the agent verifies the server through its configured CA. The application
handshake binds the TLS identity to the message exchange by checking the agent
certificate hash and deriving `DeviceID` from the peer certificate common name.

There is no separate application-layer proof signature exchange. The previously
reserved proof-message type bytes `0x12` and `0x13` were never used in
production and are now rejected by both protocol decoders. Active handshake
constants live in [`server/internal/protocol/types.go`](../../server/internal/protocol/types.go)
and [`agent/crates/mesh-protocol/src/types/handshake.rs`](../../agent/crates/mesh-protocol/src/types/handshake.rs).

### 3. `0x14` saves a round-trip, not cryptographic work

`SkipAuth` lets a reconnecting agent replay the cached CA certificate hash. If
the hash still matches, the server skips the `ServerHello` reply and the agent
begins framed control traffic immediately. If the hash is stale, the server
rejects the fast path and the agent falls back to a full `AgentHello` /
`ServerHello` handshake.

Because authentication is mTLS-only, `0x14` does not avoid certificate
verification or signatures at the application layer. It removes one handshake
round-trip and still depends on the TLS handshake for identity.

### 4. 1-RTT TLS session resumption is the TLS-cost lever

The W3 spike proved that quic-go v0.60.0 completes 1-RTT TLS session resumption
under mTLS while preserving the verified client identity server-side
(`DidResume == true` and `PeerCertificates` still populated). The paired
benchmark measured roughly 23% / 360µs lower per-reconnect cost with about 207
fewer allocations by skipping the asymmetric handshake.

0-RTT also works under mTLS on this version, but early data is replayable and
only saves latency on top of resumption. The decision is therefore:

- adopt 1-RTT TLS session resumption;
- defer 0-RTT early data;
- keep server `Allow0RTT` off; and
- track quinn agent-side session-ticket caching as residual implementation debt.

**The server needs no change to hold this decision.** quic-go issues
session tickets by default, and the spike confirms resumption against the
unmodified `ServerTLSConfig` with `Allow0RTT` off — kept off to foreclose
0-RTT replay.
[`TestQUICSessionResumption_PreservesMTLSIdentity`](../../server/internal/agentapi/quic_resumption_test.go)
is the always-run regression guard for both halves of resumption: that it
completes, and that the resumed session still carries the verified client
identity.
[`TestProductionListenerRefusesEarlyData`](../../server/internal/agentapi/server_zero_rtt_test.go)
is what holds early data off. It dials the running listener the way a machine
asking for early data would and fails if any is granted, so switching
`Allow0RTT` on turns a test red.

The agent's own `rustls::ClientConfig` defaults to an in-memory session
store and the quinn config is built once and cloned per reconnect
attempt, so resumption is available across in-process reconnects. The
server counts each connection's outcome on
`opengate_agent_tls_handshakes_total{resumed}`
([Metrics Reference](../architecture/Metrics-Reference.md#agent-connections)) —
the only faithful
observation point, since a client cannot report whether its own session
resumed and may present a ticket that is then declined.

**Measured in production, 2026-08-29.** A reconnect driven by interrupting
the agent's path to the server — with neither process restarted — resumed:
the server recorded `resumed="true"`, and the agent took the `0x14` fast
path on the same reconnect (`fast_path=true`). Both halves of this
decision are observed rather than argued.

**A server restart is always a full handshake.** Nothing pins the server's
TLS session-ticket keys, so Go generates a fresh random key per process and
a restarted server cannot decrypt a ticket its predecessor issued. The
saving is therefore earned on connection drops — a network interruption, a
path change — and never on a deploy. Pinning those keys from a Secret would
extend it to deploys; that is a separate decision and is not taken here.

One thing remains owed and is tracked in
[`techdebt.md`](../../.claude/techdebt.md): the in-memory store does not
survive a process restart.

The empirical record for resumption is
[`server/internal/agentapi/quic_resumption_test.go`](../../server/internal/agentapi/quic_resumption_test.go);
for the early-data finding it is the archived
[W3 plan](../../.claude/plans/archive/fast-path-w3-0rtt-eval.md).

## Consequences

- The W1 handshake reorder is a breaking wire-protocol change and requires a
  coordinated server + signed agent rollout.
- The server does not carry a transitional dual-mode handshake for the current
  one-agent production fleet.
- `0x12` and `0x13` are retired reservations; decoders reject them instead of
  preserving dead proof-message API surface.
- Large-tier reconnect-storm work should measure TLS session resumption,
  reconnect backoff, and gateway capacity rather than treating the removed proof
  messages as a security or performance lever.
- Future 0-RTT adoption requires a replay analysis for every early-data payload
  before enabling server-side `Allow0RTT`.

## References

- [ADR-036: Per-file ADRs Become Mutable; Current-State Docs Doctrine](ADR-036-mutable-adrs-current-state-doctrine.md)
- [W1 archived plan](../../.claude/plans/archive/fast-path-w1-client-first-handshake.md)
- [W2 archived plan](../../.claude/plans/archive/fast-path-w2-0x14-fast-path.md)
- [W3 archived plan](../../.claude/plans/archive/fast-path-w3-0rtt-eval.md)
- [Technical debt: coordinated rollout and agent ticket cache](../../.claude/techdebt.md)
