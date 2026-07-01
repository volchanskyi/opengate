# WS-1 — Protocol forward-compatibility (bidirectional) + capability negotiation

**Objective:** Make control dispatch tolerant of unknown message types **in both directions**
and add a capability handshake so the server never sends a new server→agent control variant to
an agent that cannot decode it. Without this, later additive variants break mixed-version
fleets.

**Dependencies:** none. **Blocks:** WS-3 (must merge first). **Parallel with:** WS-0, WS-2.

## Context (both directions are broken today)

- **Agent→server:** an unknown control type returns `ErrUnexpectedMessage`
  ([`conn.go:245`](../../server/internal/agentapi/conn.go#L245)) and the control loop **drops
  the connection** on any non-EOF error
  ([`server.go:260`](../../server/internal/agentapi/server.go#L260)), pinned by
  `TestAgentConn_HandleUnknownMessage` ([`conn_part8_test.go:44`](../../server/internal/agentapi/conn_part8_test.go#L44)).
- **Server→agent (the gap WS-1 originally missed):** the Rust `ControlMessage` is
  `#[serde(tag="type")]` + `#[non_exhaustive]`
  ([`control.rs:10-13`](../../agent/crates/mesh-protocol/src/control.rs#L10)). **`#[non_exhaustive]`
  does not make serde tolerate unknown tags** — an unknown tag fails at *decode*, before any
  match arm runs. So a server sending `RequestHealthWindow` (WS-3) to an old agent breaks it.

So "additive message" is wire-additive but **not** operationally safe in either direction yet.

## File inventory

- **Modify:** [`server/internal/agentapi/conn.go`](../../server/internal/agentapi/conn.go) (relax the `handleControl` `default:` arm)
- **Modify:** [`server/internal/agentapi/conn_test.go`](../../server/internal/agentapi/conn_test.go) (flip the pinning test)
- **Modify:** [`agent/crates/mesh-protocol/src/control.rs`](../../agent/crates/mesh-protocol/src/control.rs) — an `Unknown`/catch-all decode path so unknown server→agent tags deserialize-then-ignore instead of erroring
- **Modify:** capability advertisement at register (reuse the existing `AgentCapability` set in [`types/device.rs`](../../agent/crates/mesh-protocol/src/types/device.rs) + Go `protocol`); the server gates new server→agent variants on the agent's advertised capabilities

## Steps (TDD-first)

1. **Test first (Go):** rewrite `TestAgentConn_HandleUnknownMessage` to assert `handleControl`
   returns **no error** (connection survives), logs the unknown type, and a *known* message
   still dispatches after an unknown one. Change the `default:` arm to **log + return nil**;
   keep frame-level errors (non-control frame, decode failure) fatal.
2. **Test first (Rust):** an unknown-tag server→agent frame **decodes into a catch-all and is
   ignored** (loop continues), while a malformed frame is still an error. Add the catch-all
   decode path; keep `#[non_exhaustive]`.
3. **Test first (both):** capability handshake — server with a new capability + old agent that
   does not advertise it → server **does not** send the new variant; new agent that advertises
   it → server does. Implement capability checks at the send sites.
4. **Golden fixtures:** old-server/new-agent and new-server/old-agent unknown-variant cases.

## Gotchas / constraints

- Distinguish "unknown/future type" (tolerate) from "malformed frame / decode error" (fatal) and
  "known-but-wrong-direction" (strict) — only the unknown-*type* path becomes forgiving, in both
  languages.
- Capability gating is the real safety net; tolerant decode is the backstop. Both ship together.
- Keep log lines low-noise (no payload dumping; no PII).

## Reviewer checklist

- [ ] Go `default:` relaxed (no drop on unknown type); known messages still work afterward.
- [ ] Rust unknown server→agent tag decodes-and-ignores; malformed frame still fatal; `#[non_exhaustive]` kept.
- [ ] Capability handshake: new server→agent variants gated on advertised capability; tested old×new both ways.
- [ ] Bidirectional unknown-variant golden fixtures added; `make golden` green.

## Verification

`cd server && go test ./internal/agentapi/...`; `cd agent && cargo test -p mesh-protocol`;
`make golden`. `/precommit` green.
