---
number: 063
title: Server-to-agent control-message completeness
status: Accepted
date: 2026-08-03
---

# ADR-063: Server-to-agent control-message completeness

## Context

The two ends of the control plane model the same wire contract differently.
Go's `ControlMessage`
([`control.go`](../../server/internal/protocol/control.go)) is one flat union
struct in which every field but `Type` is `omitempty`, so a zero-valued field is
**dropped from the encoded map**. Rust's `ControlMessage`
([`control.rs`](../../agent/crates/mesh-protocol/src/control.rs)) is an
internally-tagged enum whose variant fields are **required at decode** unless
marked `#[serde(default)]`.

A server→agent message carrying a zero-valued field therefore encodes to a map
the agent cannot decode. The agent routes any non-`Io` decode error to a
catch-all that warns and **breaks the control loop, forcing a full QUIC
reconnect** ([`main.rs`](../../agent/crates/mesh-agent/src/main.rs)).

Four of the thirteen server→agent variants were exposed. Encoding through the
real Go codec and decoding through the real `rmp_serde` path:

| Go input | Wire bytes | Rust decode |
|---|---|---|
| `{Type: MsgRestartAgent}` | `81 a4 type ac RestartAgent` | `Err("missing field \`reason\`")` |
| `{Type: MsgAgentDeregistered}` | `81 a4 type b1 AgentDeregistered` | `Err("missing field \`reason\`")` |
| `{Type: MsgAgentUpdate, Version, URL}` | `83 …` (no `signature`) | `Err("missing field \`signature\`")` |
| `{Type: MsgSessionRequest}` | `81 a4 type ae SessionRequest` | `Err` (`token`, `relay_url`, `permissions` absent) |

Only `RestartAgent` was reachable from a client request, and it was reachable
all the way: `RestartDeviceRequest.reason` carried no `minLength`, OpenAPI
constraints are not enforced at runtime, and the handler sanitized rather than
rejected. `POST /api/v1/devices/{id}/restart` with `{"reason": ""}` returned
**200**, the device never restarted, and the agent dropped its control stream.

No gate caught it. Forward goldens verify Rust-encode → Go-decode — the wrong
direction, and they cannot see a Go **encoder** that omits a key the Rust
**decoder** requires. The reverse golden for `RestartAgent` pinned a non-empty
reason, and `AgentUpdate`, `AgentDeregistered`, `RequestHardwareReport` and
`RequestDeviceLogs` had no reverse golden at all. Handler tests asserted the
decoded reason equalled a non-empty fixture; the UI always sends one.

## Decision

**A zero-valued field is legal on the wire only where the zero value is a legal
value.** That rule splits the exposed variants in two, and each half is closed
at a different end.

### Informational fields default at decode

`reason` on `RestartAgent` and `AgentDeregistered` is informational: the agent
restarts, or cleans up and exits, either way. An empty reason is a legal
message, so both fields carry `#[serde(default)]` and decode to `""`.

The fix lands **agent-side**, not by dropping `omitempty` server-side. The union
struct shares one `Reason` field across variants, so dropping `omitempty` would
emit `reason: ""` on every message Go encodes; and the hand-written encoder
([ADR-060](ADR-060-control-message-hand-written-encoder.md)) is contractually
byte-identical to the reflection encoder, so a per-variant override would have
to break that invariant.

`AgentUpdate.sha256` keeps its existing `#[serde(default)]`, inconsistent with
its siblings below: the updater verifies it against the downloaded artifact, so
an absent value fails closed at install time rather than at decode time.

### Load-bearing fields are guarded at send

`token`, `relay_url`, `permissions`, `version`, `url` and `signature` are
load-bearing. An empty token, a session with no relay URL, or an unsigned update
is not a message worth acting on, so the agent's decoder keeps requiring them —
and the server refuses to construct the frame. `SendSessionRequest` and
`SendAgentUpdate` ([`conn.go`](../../server/internal/agentapi/conn.go)) return
`ErrIncompleteControlMessage` naming the empty field instead of writing a frame
the peer cannot decode. This is what protects agents already in the field: they
hold the strict decoder until they update, so the frame must never leave the
server.

No `Default` derive is added to `SessionToken` or `Permissions`: a defaulted
`SessionToken("")` would let an agent walk into a session with an empty token,
which is worse than a decode error.

An undeliverable manifest is a property of the manifest, not of one agent, so
`PushUpdate` surfaces the guard as **400** rather than counting a push of zero
agents as success.

### An empty restart reason is a 400

Independent of the wire fix: a 200 for a restart that provably did not happen is
a defect on its own. `RestartDeviceRequest.reason` gains `minLength: 1` and a
`maxLength` in the schema **and** a runtime check in the handler, because
OpenAPI constraints are not runtime-enforced here. A reason that carries no
printable text is refused before any frame is written; an omitted reason keeps
the server default, which is never empty.

### Every server→agent write has a reverse golden

The regression class is "a variant whose minimal wire shape the agent rejects",
so the guard is golden **completeness**, not a cross-language reflection test —
with the fix living in Rust, a Go test cannot ask whether a Rust field is
required. A Go test reflects over `*agentapi.AgentConn`'s exported `Send*`
methods and asserts each appears in an explicit method→golden table; the Rust
verifier then proves those shapes decode. Adding a server→agent write without a
golden fails the test.

Minimal-shape goldens are named `…_min` — one per variant that pins the smallest
map the server can emit, rather than a per-field matrix. The variant, not the
field, is the unit of the contract. `SessionRequest` and `AgentUpdate` get no
`_min` golden: under the send guards that frame is unconstructible, and the
guard tests are what pin it.

## Consequences

- A restart with an empty, whitespace-only, over-long, or control-character
  reason returns 400 and puts nothing on the agent's stream. Callers that
  omit the field entirely are unaffected.
- An agent that receives `RestartAgent` or `AgentDeregistered` with the reason
  dropped logs an empty reason and proceeds, instead of dropping its control
  stream and reconnecting.
- A session or update send with a load-bearing field empty fails at the server
  with a typed error the handler classifies, rather than silently degrading.
- `SessionRequest` remains reachable-but-unbroken today (the token is always
  generated, the relay URL always formatted, permissions always non-nil); the
  guard and its tests are what keep the next refactor from breaking it.
- The reverse-golden corpus grows from 16 to 22 fixtures, and every future
  server→agent write must add one.

## Alternatives considered

- **Drop `omitempty` server-side.** Emits empty keys on every message and breaks
  the byte-identity contract the hand-written encoder is held to.
- **`#[serde(default)]` on every field.** Turns an unsigned update or a
  token-less session into a silently-accepted message; the decode error is the
  cheaper failure.
- **A cross-language reflection test.** Cannot work once the requiredness lives
  in Rust: a Go test has nothing to reflect over.
