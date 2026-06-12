# Micro-plan W5 — Remove the server-opens workaround + harden + dead-code sweep

**Parent:** [`fast-path-reconnect-fix.md`](fast-path-reconnect-fix.md). **Depends on:** W1 (mechanism changed) + W2 (to know if `0x12`/`0x13` stay unused).
**Branch:** `dev`.

## Why
Once W1 makes the agent open the stream, the server-opens scaffolding and its
"workaround / mTLS bug" comments are dead and misleading. Clean them up, add the one
defensive idea the retired plan got right (a bounded accept timeout), and sweep the
vestigial handshake constants — honoring the **"clean up unused tests/configs/docs"**
rule.

## Files
- [`server/internal/agentapi/server.go`](../../server/internal/agentapi/server.go) — remove any "(server-initiated) / stream ownership workaround / AcceptStream blocks with mTLS" comments; add `context.WithTimeout` around the control-stream **accept** (defensive against a stalled peer — the only valid idea from the retired plan, framed as hardening, not a quic-go workaround).
- [`agent/crates/mesh-agent/src/main.rs`](../../agent/crates/mesh-agent/src/main.rs#L425) — remove the `// Server opens the bidirectional stream (stream ownership workaround)` comment.
- [`agent/crates/mesh-agent-core/src/connection.rs`](../../agent/crates/mesh-agent-core/src/connection.rs#L67) — remove the `#[expect(dead_code, reason = "used in QUIC reconnect flow (Phase 4D)")]` field if W1/W2 made it obsolete.
- [`server/internal/protocol/types.go`](../../server/internal/protocol/types.go#L107) **+ the Rust `mesh-protocol` equivalents** — if `MsgServerProof (0x12)` / `MsgAgentProof (0x13)` remain **unused** after W1/W2 (they were never implemented), **remove the constants** and any golden/test that references them. (Confirm W2 did not adopt them first.)
- `HandshakeResult.Skipped` — **keep** (W2 makes it real); just ensure it's no longer hardcoded `false`.

## Steps (TDD)
1. Strengthen/adjust the covering handshake test (a pure cleanup still needs the test touched first — add a `// covers client-first accept + timeout` assertion).
2. Add the bounded accept timeout; remove the stale comments.
3. Remove the vestigial `0x12`/`0x13` constants (+ Rust) and any references **iff** unused after W2.
4. `make dead-code` — must be clean (no new dead code; the swept constants gone).

## Reviewer acceptance
- [ ] No "workaround" / "mTLS bug" / "server-initiated" comments remain in the handshake path (Go + Rust).
- [ ] A bounded `context.WithTimeout` guards the control-stream accept.
- [ ] `0x12`/`0x13` constants removed (Go + Rust) if unused, with their references/goldens; `make dead-code` clean.
- [ ] `/precommit` gauntlet green.

## Out of scope
The handshake reorder (W1), `0x14` (W2), 0-RTT (W3), ADR (W4).
