# WS-1 — Protocol forward-compatibility (dispatch tolerates unknown control types)

**Objective:** Make the agent→server control dispatch ignore-and-continue on unrecognized
message types so later additive `ControlMessage` variants are operationally safe across mixed
agent/server versions.

**Dependencies:** none. **Blocks:** nothing hard, but should **merge first**. **Parallel
with:** WS-0, WS-2.

## Context

Today an unknown control type returns `ErrUnexpectedMessage`
([`conn.go:245`](../../server/internal/agentapi/conn.go#L245)) and the control loop
**drops the connection** on any non-EOF error
([`server.go:260`](../../server/internal/agentapi/server.go#L260)). This is pinned by
`TestAgentConn_HandleUnknownMessage` ([`conn_test.go:612`](../../server/internal/agentapi/conn_test.go#L612)).
So "additive message" is wire-additive but **not** operationally safe yet.

## File inventory

- **Modify:** [`server/internal/agentapi/conn.go`](../../server/internal/agentapi/conn.go) (the `handleControl` `default:` arm)
- **Modify:** [`server/internal/agentapi/conn_test.go`](../../server/internal/agentapi/conn_test.go) (flip the pinning test)

## Steps (TDD-first)

1. **Test first:** rewrite `TestAgentConn_HandleUnknownMessage` to assert `handleControl`
   returns **no error** (connection survives) and that the unknown type is logged. Add a
   second case proving a *known* message still dispatches normally after an unknown one.
2. Change the `default:` arm to **log at warn/debug and return nil** (continue the loop)
   instead of returning `ErrUnexpectedMessage`. Keep frame-level errors (non-control frame,
   decode failure) as hard errors — only *unrecognized control message type* becomes tolerant.

## Gotchas / constraints

- Distinguish "unknown/future type" (tolerate) from "malformed frame / decode error" (still
  fatal) and "known-but-wrong-direction" — keep the latter two strict; only the type switch
  `default` becomes forgiving.
- Keep the log line low-noise (no payload dumping; no PII).

## Reviewer checklist

- [ ] Test flipped first; asserts no-error + continues + logs.
- [ ] Only the control-type `default` is relaxed; frame/decode errors stay fatal.
- [ ] No connection drop on unknown type; known messages still work afterward.

## Verification

`cd server && go test ./internal/agentapi/...`. `/precommit` green.
