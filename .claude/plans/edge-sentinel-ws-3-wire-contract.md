# WS-3 — Wire contract (additive ControlMessage variants, golden-gated)

**Objective:** Add the additive, tenant-tagged telemetry messages to the MessagePack control
protocol, with Rust→Go golden fixtures, reusing the existing single control stream.

**Dependencies:** WS-2 (field shapes), WS-1 (forward-compat so older servers tolerate).
**Blocks:** WS-4. 

## Context

Protocol is MessagePack control frames, `[1-byte type][4-byte BE len][payload]`, golden-file
drift-guarded both languages. Rust enum is `#[serde(tag="type")]`, `#[non_exhaustive]`
([`control.rs`](../../agent/crates/mesh-protocol/src/control.rs)); precedent is the
existing `HardwareReport`. Reuse `FRAME_CONTROL` — **no new QUIC stream** (ADR-005/037
single-control-stream constraint).

## File inventory

- **Modify:** [`agent/crates/mesh-protocol/src/control.rs`](../../agent/crates/mesh-protocol/src/control.rs) (new variants)
- **Modify:** [`agent/crates/mesh-protocol/tests/golden_test.rs`](../../agent/crates/mesh-protocol/tests/golden_test.rs) (+ `testdata/golden/*.bin`)
- **Modify:** Go [`server/internal/protocol/types.go`](../../server/internal/protocol/types.go), [`server/internal/protocol/golden_test.go`](../../server/internal/protocol/golden_test.go)
- **Modify:** [`agent/crates/mesh-agent/src/main.rs`](../../agent/crates/mesh-agent/src/main.rs) (emit from the 60 s heartbeat loop)

## New variants (agent→server unless noted)

- `AgentHealthSummary { ts, org_id, node_anomaly_rate, per_family_rates, recent_bitmask (serde_bytes), sampler_ver, model_ver }`
- `AgentMetricWindow { ts, org_id, dims: [{name, avg}] }` (10 s windows) — **`avg` only on the live
  wire** (central = avg, the cardinality decision). **min/max/last + 1 s raw are NOT sent live**;
  they live in the agent-local TSDB (WS-14b) and surface via the on-demand `RequestLocalHistory`
  pull (WS-15). Per-dim names are **per-entity-capped** (aggregate/top-N cores/disks/interfaces).
- `ProcessReport { ts, org_id, top_n: [{rank, basename, cmdline_hash?, pid, cpu, mem}] }`
  (**no full cmdline** — basename + optional hash only; full cmdline is on-demand/audited, not here)
- `RequestHealthWindow` (server→agent) / `HealthWindowResponse` (bounded recent window) —
  **the server→agent variant is gated on an advertised agent capability** (WS-1)

## Steps (TDD-first)

1. **Test first (Rust):** add golden generation for each new variant; round-trip encode/decode.
2. Add the variants to the Rust enum (keep `#[non_exhaustive]`, `serde(tag="type")`,
   `rmp_serde` named); regenerate `testdata/golden/*.bin`.
3. Mirror the Go structs in `types.go`; **Go golden test verifies** the Rust-generated bins.
4. Emit `AgentHealthSummary`/`AgentMetricWindow`/`ProcessReport` from the heartbeat loop at a
   configurable interval floor (default-off until WS-4 handler exists).

## Gotchas / constraints

- Additive only; never renumber/repurpose existing types. `serde_bytes` for the bitmask.
- `org_id` travels in-message (defense-in-depth); server still authoritatively scopes by the
  connection's enrolled device→org (do not trust agent-supplied org for authz).
- **Telemetry payload cap ≈ 64 KiB** (far below the 16 MiB frame max) + interval floor; telemetry
  reuses `FRAME_CONTROL`, so operational control messages take **priority** and telemetry is
  **dropped** (with a counter) before it can backpressure restart/session/heartbeat traffic.
- **Per-agent write arbitration (a cap alone is not scheduling):** a bounded per-agent write queue
  with **priority classes** (interactive terminal/file ops > control > telemetry/backfill); telemetry
  is deferred/dropped before interactive traffic. Measure terminal/file-op p99 under concurrent
  telemetry + backfill (feeds the WS-8 budget).
- `RequestHealthWindow` is sent only to agents that advertised the capability (WS-1).

## Verification

`make golden`; `cd agent && cargo test -p mesh-protocol`; `cd server && go test ./internal/protocol/...`. `/precommit` green. `/docs`: Wire-Protocol page.

## Hand-off

**Deps:** WS-1 ✅ WS-2 ✅ (both landed in `df257eb`). **Blocks:** WS-4. **Gate:** `make golden` + `/precommit`.

### Start-here orientation (grounding the plan omits)

The wire is one flat struct per side, not per-variant types. Adding a variant touches four things, in TDD order:

1. **Rust enum** — [`control.rs`](../../agent/crates/mesh-protocol/src/control.rs): add struct-style variants *after* the existing ones, keeping `#[serde(tag = "type")]` and `#[non_exhaustive]`. The `Unknown` `#[serde(other)]` arm stays **last**. Precedent to copy: `HardwareReport`.
2. **Go type constants + flat struct** — ⚠️ the constants and the flat `ControlMessage` struct live in [`control.go`](../../server/internal/protocol/control.go) (`MsgRequestHardwareReport…`, struct with `omitempty` msgpack fields), **not** `types.go`. New `MsgAgentHealthSummary`-style consts and new fields go there. Only new **capabilities** go in [`types.go`](../../server/internal/protocol/types.go), next to `CapHardwareInventory`/`CapDeviceLogs`.
3. **New capability** for server→agent `RequestHealthWindow`: add e.g. `CapHealthWindow` in both `types.go` (Go) and the Rust `AgentCapability`, then gate the send with `requireCapability(...)` — copy the WS-1 pattern in [`conn.go`](../../server/internal/agentapi/conn.go) `SendRequestHardwareReport`, and map its `IsCapabilityError` to an "unavailable" response like [`handlers_device_inventory.go`](../../server/internal/api/handlers_device_inventory.go).
4. **Dispatch** — extend the `switch msg.Type` in [`conn.go`](../../server/internal/agentapi/conn.go) `handleControl`. Since WS-1 the `default` arm *ignores* unknown types, so a half-wired variant fails **silent** — the golden test is the real guard, not the dispatch.

### Golden workflow (Rust generates, Go verifies — this direction)

- Generate: `cd agent && GENERATE_GOLDEN=1 cargo test -p mesh-protocol` → writes `testdata/golden/*.bin`. **Commit the `.bin` files.**
- Verify parity: `make golden`, then `cd server && go test ./internal/protocol/...` (Go reads the Rust-generated bins).
- Go golden tests are split across `golden_test.go` + `golden_part2…part7_test.go`. Add the new case in whichever part file keeps functions co-located; **don't** create another `_partN` file for one case.

### Non-obvious correctness constraints (easy to miss in review)

- **Never trust agent-supplied `org_id` for authz.** It rides in-message for defense-in-depth only; the server scopes by the connection's enrolled device→org. The agent read-loop is pinned to `WithDefaultTenant` ([`server.go`](../../server/internal/agentapi/server.go)), so a payload `org_id` that disagrees must not widen scope — WS-4 persists under the connection's org, not the payload's.
- **`serde_bytes` on `recent_bitmask`** — without it `rmp_serde` encodes `Vec<u8>` as an array of ints, not a bin blob, and the Go `[]byte` decode mismatches. Likely first golden failure.
- **Telemetry shares `FRAME_CONTROL`** — no new QUIC stream (ADR-005/037). The priority-queue/drop-with-counter arbitration (interactive terminal/file ops > control > telemetry) is load-bearing; a cap alone is not scheduling. Any deferral must be written down, not omitted.
- **Emission default-off** until WS-4's handler exists — same discipline as WS-2's `--edge-sentinel` flag in [`main.rs`](../../agent/crates/mesh-agent/src/main.rs).

### Reviewer checklist

- [ ] Variants appended, not reordered/repurposed; Rust `#[non_exhaustive]` + `#[serde(other)] Unknown` arm still last.
- [ ] Go consts + flat-struct fields in **`control.go`** (not `types.go`); capabilities in `types.go`; all new fields `omitempty`.
- [ ] `recent_bitmask` uses `serde_bytes` (Rust) ↔ `[]byte` (Go); golden bin confirms binary encoding.
- [ ] Rust-generated goldens committed; `make golden` + `go test ./internal/protocol/...` green.
- [ ] `RequestHealthWindow` gated via `requireCapability` on a **new advertised capability**; `IsCapabilityError` mapped to an "unavailable" response; positive **and** capability-absent negative tests.
- [ ] `ProcessReport` = basename + optional `cmdline_hash` only — no raw cmdline on the wire (grep struct + golden).
- [ ] Agent-supplied `org_id` proven non-authoritative (test: mismatched payload org does not widen server scope).
- [ ] Emission default-off; interval floor + ≈64 KiB payload cap enforced; telemetry drop-counter present or deferral written down.
- [ ] `/docs` Wire-Protocol page updated (link, don't paraphrase the field list).
