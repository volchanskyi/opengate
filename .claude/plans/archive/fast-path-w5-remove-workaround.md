# Micro-plan W5 — Remove the server-opens workaround + harden + dead-code sweep

**Status:** Complete. Archived after the bounded accept, proof-byte retirement, and dead-code sweep landed.
**Parent:** `fast-path-reconnect-fix.md`. **Depends on:** W1 + W2 — **both landed** (see [`phases.md`](../../phases.md)). Ran in parallel with W4.
**Branch:** `dev`.

## Why
W1 made the agent open the stream, so the server-opens scaffolding is dead. **Most
of the "workaround / mTLS bug" comments were already removed by W1** — this
micro-plan finishes the job: confirm none remain, add the one defensive idea the
retired plan got right (a bounded accept timeout), and sweep the vestigial
handshake constants W2 confirmed unused.

## Current state (verified 2026-06-18 — do not re-discover)
- A grep for `workaround|mtls bug|server-initiated|server opens|stream ownership`
  across the handshake path returns **only** [`conn.go:43`](../../../server/internal/agentapi/conn.go) —
  an accurate note about a mutex guarding concurrent `sendControl` calls, **not** a
  server-opens workaround. Leave it (optionally reword to drop "server-initiated" if
  it reads as stale). **No `server.go` / `main.rs` workaround comments remain.**
- `0x12`/`0x13` (`MsgServerProof` / `MsgAgentProof`) are **unused in production** —
  W2's auth model is mTLS-only. The final Go regression lives in
  [`handshake_codec_test.go`](../../../server/internal/protocol/handshake_codec_test.go)
  and pins those retired type bytes as rejected. Rust had full enum variants in
  [`handshake.rs`](../../../agent/crates/mesh-protocol/src/types/handshake.rs) plus a
  [`codec_test.rs`](../../../agent/crates/mesh-protocol/tests/codec_test.rs) round-trip.
  No `testdata/golden` fixtures reference proof messages.
- [`connection.rs`](../../../agent/crates/mesh-agent-core/src/connection.rs)
  `AgentConnection.config` is **still dead** (`#[expect(dead_code, reason = "…Phase 4D")]`)
  after W1/W2.
- `HandshakeResult.Skipped` is real (W2) — **keep**.
- **W3 decision:** the server needs **no** resumption change — `Allow0RTT` stays
  **off**. Do **not** add resumption config in this cleanup.

## Files
- [`server/internal/agentapi/server.go`](../../../server/internal/agentapi/server.go) —
  add a bounded `context.WithTimeout` around the control-stream **accept**
  (`acceptControlStream` / `AcceptStream`): defensive against a stalled peer, framed
  as hardening (not a quic-go workaround). Confirm no stale comments remain.
- [`server/internal/protocol/types.go`](../../../server/internal/protocol/types.go) —
  remove `MsgServerProof` / `MsgAgentProof` (0x12/0x13) + the two test references.
- [`agent/crates/mesh-protocol/src/types/handshake.rs`](../../../agent/crates/mesh-protocol/src/types/handshake.rs) —
  remove the `ServerProof` / `AgentProof` variants + their `message_type` /
  `encoded_len` / encode / decode arms + the `codec_test.rs` cases. Run `make golden`
  — confirm zero `testdata/golden` drift.
- [`agent/crates/mesh-agent-core/src/connection.rs`](../../../agent/crates/mesh-agent-core/src/connection.rs) —
  wire `config` into the reconnect flow if W2 needs it there; otherwise remove the
  field + the `new()` parameter and drop the `#[expect(dead_code)]`.

## Steps (TDD)
1. Strengthen the covering handshake test first (a pure cleanup still touches the
   test — add a `// covers client-first accept + bounded timeout` assertion).
2. Add the bounded accept timeout.
3. Remove the `0x12`/`0x13` constants/variants (Go + Rust) + references; `make golden`
   clean.
4. Resolve `connection.rs` `config` (wire or remove).
5. `make dead-code` clean (no new dead code; swept constants gone).

## Reviewer acceptance
- [x] grep for `workaround|mTLS bug|server opens|stream ownership` in the handshake
      path (Go + Rust) is clean.
- [x] A bounded `context.WithTimeout` guards the control-stream accept.
- [x] `0x12`/`0x13` removed (Go + Rust) with references; `make golden` + `make dead-code`
      clean.
- [x] `connection.rs` `config` no longer `#[expect(dead_code)]` (wired or removed).
- [x] `Allow0RTT` not introduced (W3 keeps it off); `/precommit` green.

## Out of scope
Handshake reorder (W1), `0x14` (W2), resumption (W3), ADR (W4).
