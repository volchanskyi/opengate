# [RETIRED 2026-06-11] QUIC Stream Ownership Workaround — Revert Plan

> **This plan is retired. Its root-cause analysis was empirically disproven.**
>
> The plan assumed an "mTLS-specific quic-go bug where `AcceptStream` blocks" that a
> future quic-go release would fix, after which stream ownership could be reverted.
> A TLS/mTLS matrix on quic-go **v0.60.0** showed the deadlock is **not**
> mTLS-specific and **not** a quic-go bug — it is standard RFC 9000 §2 stream
> discovery (the stream opener must write first). The "20,000-agent" and
> "5,000-agent" figures were scaling-tier numbers from the design doc (§4.3), not
> stream-ownership benchmarks; the `0x14` fast path was never implemented; and the
> Rust agent already calls `accept_bi()` in production. Reverting as written would
> have re-created a permanent deadlock.
>
> **Superseded by:** [`fast-path-reconnect-fix.md`](../fast-path-reconnect-fix.md)
> (the real fix — client-first handshake + the `0x14` fast path) and
> [`docs/Multiscale-Readiness.md`](../../../docs/Multiscale-Readiness.md) (the
> scaling context). The one salvageable idea — a **bounded context** around the
> stream open/accept — survives as W5 of the master plan (defensive, not a
> quic-go workaround).
>
> Original content preserved below for history. **Do not act on it.**

---

## Status: DEFERRED — waiting for quic-go update  *(historical; premise was false)*

## Problem (as originally — and incorrectly — diagnosed)

With `quic-go` v0.48.2 (and confirmed on v0.59.0), when mTLS client certificates are
used, `AcceptStream` on the server blocks indefinitely until the client writes data on
the stream. This creates a deadlock with our protocol, which requires the server to
send `ServerHello` first. *(Reality: the block happens with **any** TLS mode — it is
the opener-must-write-first rule, not mTLS.)*

## Current Workaround (applied in Phase 4, commit 97cb935)

Reversed stream ownership — the **server** calls `OpenStreamSync` and the **client**
calls `AcceptStream` for the control stream. *(Reality: this was the original Phase 4
implementation, applied to dodge the misdiagnosed deadlock; it forecloses the designed
agent-initiated `0x14` fast path.)*

### Originally claimed architecture impact (from design doc §3.3, §4.3)
- Fast-path reconnection (`[0x14]` cached cert hash) gains an extra round-trip
  *(the fast path was never actually implemented)*
- QUIC stream IDs are odd (server-initiated) instead of even (client-initiated)
- ">20,000 agents → goroutine pressure" / "5,000+ simultaneous stream opens on k8s
  restart" *(these are §4.3 scaling-tier figures, not stream-ownership benchmarks)*

## Originally proposed revert (DO NOT DO — would re-deadlock)
Revert `server.go` to `AcceptStream` and the integration test to `OpenStreamSync`,
"when quic-go fixes it." The premise is false; the correct fix is the client-first
handshake in the master plan, not a revert.

## The one valid idea
Add a bounded `context.WithTimeout` around the stream open/accept to prevent goroutine
leaks under adversarial peers — kept as W5 of the master plan, framed as defensive
hardening rather than a quic-go workaround.
