# Micro-plan W1 — Client-first QUIC handshake

**Parent:** `fast-path-reconnect-fix.md` (master, active plan). **Order:** first — unblocks W2/W3/W5.
**Branch:** `dev`. **Owner:** implementing engineer. **Reviewer:** Ivan.

## Why
The control stream uses **server-opens / server-speaks-first**, a Phase-4 workaround
for a deadlock misdiagnosed as a quic-go mTLS bug (it's RFC 9000 stream-discovery —
proven on v0.60.0). Realign to the designed **agent-opens / agent-speaks-first**:
removes the deadlock the QUIC-correct way, restores **even (client-initiated)** stream
IDs, and unblocks the `0x14` fast path (W2).

## Implementation reality (verified — read before coding)
The handshake is **mTLS + one nonce/cert-hash exchange**, NOT a signature handshake:
- `0x10` ServerHello (server_nonce + CA cert hash) → `0x11` AgentHello (agent_nonce +
  agent cert hash) → server verifies agent cert hash == TLS peer cert + extracts
  DeviceID from the peer cert CN ([handshaker.go:47-90](../../../server/internal/agentapi/handshaker.go#L47)).
- `0x12`/`0x13`/`0x14` are **defined constants, never implemented**. Auth is the TLS
  layer (`RequireAndVerifyClientCert`). The nonces are exchanged but not signed over.
- So this reorder is **low cryptographic risk** — there are no signatures whose
  nonce-ordering must be re-derived; it's a message-order + stream-ownership flip.

## Files
- [`server/internal/agentapi/handshaker.go`](../../../server/internal/agentapi/handshaker.go) — **reorder**: read `0x11` AgentHello **first** (`io.ReadFull`), validate, then `stream.Write` the `0x10` ServerHello. (Today it writes `0x10` then reads `0x11`.)
- [`server/internal/agentapi/server.go`](../../../server/internal/agentapi/server.go) — `openControlStream` → **accept** the stream (`conn.AcceptStream(ctx)`), rename accordingly; the agent now opens.
- [`agent/crates/mesh-agent/src/main.rs`](../../../agent/crates/mesh-agent/src/main.rs#L425) — `conn.open_bi()` instead of `accept_bi()`; **write AgentHello first**, then read ServerHello (the send/recv handshake lives just after the stream is obtained — in `main.rs` / `mesh-agent-core` connection handshake).
- `agent/crates/mesh-protocol` — confirm AgentHello-first encode/decode (the message types exist; only the *send order* changes).
- **Golden files** — `server/internal/protocol/testdata/golden` + the Rust `golden_test`: regenerate via `make golden` (the on-wire send order changes).
- [`server/tests/integration/agentapi_test.go`](../../../server/tests/integration/agentapi_test.go) — `connectAgent` uses `OpenStreamSync` (agent opens) instead of `AcceptStream`.

## Steps (TDD — failing test first)
1. Add a failing handshake test (Go `handshaker_test.go` + integration `connectAgent`; Rust) asserting the **agent opens + writes first** and the handshake completes — run it under **both** server-only TLS and mTLS (mirror the throwaway matrix from the master plan §2).
2. Reorder `handshaker.go` (read `0x11` → write `0x10`); keep cert-hash verification + DeviceID extraction unchanged.
3. `server.go`: accept the control stream.
4. Rust agent: `open_bi` + write AgentHello first.
5. `make golden` → regenerate; cross-language golden tests green.
6. Update the integration test to open the stream agent-side.
7. Assert control-stream IDs are **even** on a live handshake.

## Decision required
**Rollout of a breaking wire change.** A client-first server cannot handshake with a
server-first agent. Choose: (a) **coordinated cutover** — deploy the server and push
the agent auto-update together (prod has **one** agent, Ed25519-signed updates,
Phase 14); or (b) **transitional dual-mode** — server peeks: if the first frame
arrives it accepts, else it opens (back-compat during rollout). Recommend (a) given a
single agent; document the choice.

## Reviewer acceptance
- [ ] Handshake completes **agent-opens** under server-only TLS *and* mTLS, no deadlock (v0.60.0).
- [ ] Control-stream IDs are **even** (client-initiated).
- [ ] Cross-language golden tests regenerated + green (Rust ↔ Go).
- [ ] The production agent reconnects across the cutover with **no manual re-enrollment**.
- [ ] `/precommit` gauntlet green.

## Out of scope (other micro-plans)
`0x14` fast path → **W2**. 0-RTT/resumption → **W3**. ADR → **W4**. Workaround-comment
removal + bounded timeout → **W5**.
