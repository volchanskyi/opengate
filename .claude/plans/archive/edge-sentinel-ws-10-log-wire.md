# WS-10 — Log wire contract (rate dims + extended on-demand query)

**Objective:** Carry log-rate signals over the existing telemetry path and extend the on-demand
log query to host sources — additive, capability-gated, golden-tested.

**Dependencies:** WS-3 (wire + payload caps), WS-9 (rate shapes), WS-1 (capability negotiation).
**Blocks:** WS-11.

## Context

Protocol is MessagePack control frames, golden-gated both languages. `RequestDeviceLogs` already
exists ([control.rs:166](../../../agent/crates/mesh-protocol/src/control.rs#L166),
[control.go:42](../../../server/internal/protocol/control.go#L42)); this WS extends its filter and
adds log-rate dims. Rate dims **reuse WS-3 `AgentMetricWindow`** (no new variant if the dim schema
fits); only the on-demand request grows.

## File inventory

- **Modify:** [`control.rs`](../../../agent/crates/mesh-protocol/src/control.rs) — extend
  `RequestDeviceLogs` filter (source selector + structured fields); confirm rate dims fit
  `AgentMetricWindow`.
- **Modify:** Go [`control.go`](../../../server/internal/protocol/control.go) +
  [`types.go`](../../../server/internal/protocol/types.go) — mirror.
- **Modify:** goldens — [`golden_test.rs`](../../../agent/crates/mesh-protocol/tests/golden_test.rs) +
  [`golden_test.go`](../../../server/internal/protocol/golden_test.go) (+ `testdata`).
- **Modify:** [`main.rs`](../../../agent/crates/mesh-agent/src/main.rs) — emit rate dims from the
  heartbeat loop (default-off until WS-11 handler exists).

## Steps (TDD-first)

1. **Test first (Rust):** golden round-trip for the extended `RequestDeviceLogs` filter + the
   log-rate dims → add fields (additive; `#[non_exhaustive]` kept); regenerate goldens.
2. **Test first (cross-lang):** Go golden verifies the Rust-generated bins; an old agent **without**
   the log capability is not sent the extended request (capability-gated, WS-1).
3. Emit rate dims at the configurable interval floor; respect the **WS-3 64 KiB cap + priority/drop**.

## Gotchas / constraints

- Additive only; never renumber/repurpose. Rate dims carry counts/level/rank — **no message text**.
- The on-demand request is capability-gated; logs reuse `FRAME_CONTROL` (no new QUIC stream).
- Telemetry priority/drop from WS-3 applies — log bursts must not backpressure control.

## Reviewer checklist

- [ ] Extended `RequestDeviceLogs` + rate dims additive; goldens green (`make golden`).
- [ ] Capability-gated; old×new tolerated both ways (WS-1).
- [ ] Reuses `FRAME_CONTROL`; 64 KiB cap + priority/drop honored; emission default-off.

## Verification

`make golden`; `cd agent && cargo test -p mesh-protocol`; `cd server && go test ./internal/protocol/...`.
`/precommit` green. `/docs`: Wire-Protocol page.
