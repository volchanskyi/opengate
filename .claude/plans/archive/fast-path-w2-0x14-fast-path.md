# Micro-plan W2 — `0x14` fast-path reconnect + the auth decision

**Parent:** `fast-path-reconnect-fix.md` (active master plan). **Depends on:** W1 (client-first handshake).
**Branch:** `dev`.

## Why + honest scoping (read first)
The design's `0x14` fast path was meant to *"skip the full signature exchange"* on
reconnect. **But the implementation has no app-layer signatures** — auth is TLS mTLS
+ a `0x10`/`0x11` nonce/cert-hash exchange (verified; `0x12`/`0x13` are unused
constants). So `0x14` skips **the `0x10`/`0x11` round-trip + cert-hash exchange**, not
signatures. **The dominant per-reconnect cost is the TLS mTLS handshake itself, which
`0x14` does NOT avoid — that's W3 (0-RTT/resumption).** Therefore:

> **W2 is a modest optimization on its own.** Its real payoff is in combination with
> W3. The reviewer should explicitly decide whether W2 is worth doing independently,
> or only alongside W3. This micro-plan implements it cleanly; it does not oversell it.

## The auth decision this settles (for W4's ADR)
The master plan's open question — *"do you need both mTLS client-cert verify AND the
[0x10–0x13] signature exchange per reconnect?"* — is **already answered by the code**:
there **is** no app-layer signature exchange; auth is **mTLS-only + cert-hash binding
+ DeviceID from the peer cert**. W2 must record this as the settled model (and that
`0x14` therefore changes round-trips, not cryptographic cost).

## Files
- [`server/internal/agentapi/handshaker.go`](../../../server/internal/agentapi/handshaker.go) — branch on the **first** received message type: `0x14` (`MsgSkipAuth`) → read the cached CA hash, verify it equals the **current** CA cert hash, **skip** the `0x10`/`0x11` exchange, still extract DeviceID from the TLS peer cert, set `HandshakeResult.Skipped = true`. `0x11` → the W1 full handshake.
- [`agent/crates/mesh-agent/src/main.rs`](../../../agent/crates/mesh-agent/src/main.rs) + `mesh-agent-core` — cache the server's CA cert hash from a prior ServerHello; on **reconnect**, open the stream and send `0x14` + cached hash first; on hash-mismatch rejection, fall back to the full handshake.
- `HandshakeResult.Skipped` ([handshaker.go:103](../../../server/internal/agentapi/handshaker.go#L103)) — wire it for real (today hardcoded `false`).
- `agent/crates/mesh-protocol` — `SkipAuth` encode/decode already exists (`0x14`).
- Golden files for the `0x14` path (`make golden`).

## Steps (TDD)
1. Failing tests: (a) valid cached hash → `Skipped == true`, no `0x10`/`0x11` bytes exchanged; (b) **stale/absent** hash → server rejects, agent falls back to the full handshake and succeeds; (c) mTLS is still enforced on the fast path.
2. Implement the server `0x14` branch; implement agent caching + `0x14`-first on reconnect + fallback.
3. Regenerate goldens; cross-language green.

## Reviewer acceptance
- [ ] Valid cached hash → fast path (`Skipped==true`), fewer round-trips; stale hash → full-handshake fallback.
- [ ] mTLS still enforced on the fast path (no auth weakening).
- [ ] The "no app-layer signatures; mTLS-only" model recorded for W4.
- [ ] Reviewer's explicit call recorded: ship W2 standalone, or only with W3.
- [ ] Goldens green; `/precommit` green.

## Out of scope
The TLS-handshake cost reduction (0-RTT/resumption) → **W3**. ADR → **W4**.
