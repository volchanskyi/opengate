# EF-C1 — `AgentAlert` / `AlertEvidence`: the wire contract and self-contained evidence

**Master plan:** `edge-first-telemetry-and-investigations.md` §6.6 (evidence), §7.5, §7.6 (I3, I5),
D17, D22, D29, E11, E12, steps 15 and 18 (evidence half).
**Acceptance criteria owned:** **C13**, **C14**, **C9**, **C10**.
**Dependencies:** EF-B6 (the alert sink), EF-B7 (`ranked[]` comes from the edge correlation engine).
**Blocks:** EF-C2, EF-C3, EF-C4, EF-C5, EF-C6.

## Context

Because central never holds the detail behind a signal, an alert must be **self-contained (I3)**:
its evidence ships with it at fire time and nothing can be fetched later. `AgentAlert` is the
**single** alert transport (D22) — the WS-19 `AlertBreach`-on-`AgentHealthSummary` path continues
unchanged for the duration of this program (§14.2 defers touching the series it feeds), so the alert
*store* has exactly one writer and there is no second source of truth for incidents.

```
Agent → Server  AgentAlert {
  alert_id, rule_id, rule_version, severity,
  metric?, value?, window_start_ts, window_end_ts, observed_ts,
  backfilled, evidence?
}
AlertEvidence { ranked[], series[], processes[], log_samples[], truncated }
```

## Evidence composition — fixed, not "top-N"

| Field | Bound | Why |
|---|---|---|
| `ranked[]` | **8** dimensions | The KS ranking's useful tail; beyond 8 the scores are noise |
| `series[]` | the **top 3** ranked dims, event window **±5 min**, **≤ 512 points** each | Enough to see the shape either side of the event at edge resolution |
| `processes[]` | **10** at the event instant | Matches the existing `ProcessReportEntry` rank vocabulary |
| `log_samples[]` | **20** redacted lines | Bounded **before** redaction, so a flood cannot defeat the cap |
| Total | **≤ 64 KB compressed** | Truncate with `truncated: true`; **never reject** (E11) |

**Codec: DEFLATE**, `evidence_codec = "deflate-1"`. `flate2` is already in the agent's lock (the
`edge-tsdb` cold tier) and Go reads it from `compress/flate` in stdlib — **no new dependency on
either side**. The codec is a versioned string on the row, so a future change is additive.

**Severity is a closed set:** `info | warning | critical`.

## Conflict this plan must resolve (flag to the owner)

The server's telemetry payload bound is **`maxTelemetryPayloadBytes = 64 * 1024`**
([conn_telemetry.go:15](../../../server/internal/agentapi/conn_telemetry.go#L15)) — the *same* number as
the evidence cap. A maximal alert (64 KB evidence **plus** its envelope) therefore exceeds the bound
and is dropped as `payload_too_large`, silently defeating E11's "truncate, never reject".

**Resolution:** the alert path gets its own explicit bound, `maxAlertPayloadBytes`, defined as the
evidence cap plus a stated envelope headroom, with a test that a **maximal** alert is accepted and an
over-cap one is rejected with a counted drop. Do not raise the telemetry bound to fix this — they are
different paths with different risk.

## File inventory

- **Modify:** [control.rs](../../../agent/crates/mesh-protocol/src/control.rs) — `AgentAlert`,
  `AlertEvidence`, `Alerts` capability.
- **Create:** `agent/crates/mesh-agent-core/src/alerts/evidence.rs` — composition, caps, redaction,
  DEFLATE encode.
- **Modify:** [alerts/sink.rs](../../../agent/crates/mesh-agent-core/src/alerts/) — the sink's transport
  attaches here.
- **Modify:** [control_encode.go](../../../server/internal/protocol/control_encode.go) — per-field
  encoder arms; bump `controlFieldCount` (**86 today**; read it at implementation time).
- **Create:** [testdata/golden/](../../../testdata/golden/) `control_agent_alert*.bin` + `.meta.json` —
  **forward** fixtures (Rust encodes → Go decodes), which is the direction an agent→server message
  travels. The `go_control_*` reverse set and its
  [completeness guard](../../../server/internal/agentapi/golden_completeness_test.go) cover
  server→agent `Send*` writes and are **not** where `AgentAlert` belongs.
- **Docs:** [Wire-Protocol.md](../../../docs/architecture/Wire-Protocol.md).

## Steps (TDD-first)

1. **Test first (C13):** golden round-trip for `AgentAlert` with and without evidence — Rust encodes,
   Go decodes, and the Go re-encode is byte-identical. Include the smallest emittable shape, because
   `omitempty` drops zero-valued fields and that map must still decode
   ([ADR-063](../../../docs/adr/ADR-063-server-to-agent-control-message-completeness.md) is the
   cautionary tale for the other direction).
2. **Test first (C14):** an evidence blob written by the agent round-trips through Go's
   `compress/flate`, and decodes to **exactly** 8 ranked dims, 3 series of ≤ 512 points, 10
   processes, 20 log lines. Assert the *numbers*, from named constants on both sides.
3. **Test first (C9, E11):** evidence that would exceed 64 KB compressed is **truncated** with
   `truncated: true` and still accepted — never rejected wholesale. Truncation order must be
   deterministic and tested (drop from the least valuable end, not "whatever fits").
4. **Test first (C10, E12):** no evidence field can carry an unredacted log line or cmdline. Use a
   hostile corpus — bearer token, AWS-key shape, `password=…`, an email, a UNC path with credentials
   — and assert **field by field** that none survives, including inside `series[]` labels and
   `processes[]` basenames. Redaction happens at the **edge** (ADR-049); a server-side check is
   defense in depth, not the guarantee.
5. **Test first — the payload bound:** a maximal alert is accepted; an over-bound one increments a
   counted drop. This is the conflict above, pinned.
6. **Test first — severity:** an alert carrying a severity outside `info|warning|critical` is
   rejected at ingest with a counted drop (I5 — untrusted edge input, bounded and accounted).
7. **Test first — idempotency key present:** every alert carries
   `(device, rule_id, rule_version, window_start)` so EF-C2's unique index can dedup a reconnect
   replay (E7). An alert missing any component is rejected, not stored with a null.
8. Implement composition, encoding, wire, encoder arms, goldens, docs.

## Traps

- **`controlFieldCount` collision with EF-B8.** Both add wire fields. Whoever lands second rebases
  **then** regenerates goldens — regenerating before the rebase produces fixtures that pass locally
  and fail in CI.
- Rust decodes internally-tagged enums with **required** variant fields; new optional server→agent
  fields need `#[serde(default)]` agent-side (ADR-063). Getting this wrong drops the control stream.
- `flate2` must stay on the **existing** feature set — the shipped agent depends
  `default-features=false`, and pulling a new backend in would change the `deny.toml` picture.
- Compressed size is not knowable until after encoding: compose → encode → measure → truncate →
  re-encode. Test the second pass, because a naive single pass silently ships over-cap blobs.
- Evidence is **immutable** once written. Nothing in this program mutates a blob; keep the type free
  of setters so that stays true by construction.

## Out of scope

Storage, ingest accounting and the organization ceiling (EF-C2). Grouping (EF-C3). Any second alert transport
(D22 forbids it).

## Reviewer checklist

- [ ] Goldens both directions, `…_min` shape included, completeness table updated.
- [ ] Composition numbers asserted from shared named constants; DEFLATE round-trips agent → Go.
- [ ] Over-cap evidence truncates deterministically and is still accepted.
- [ ] Hostile-corpus redaction asserted field by field, edge-side.
- [ ] Alert payload bound is its own constant, sized from the evidence cap, with both cases tested.
- [ ] Severity closed set enforced at ingest; idempotency key mandatory.

## Verification

`cd agent && cargo test -p mesh-protocol -p mesh-agent-core`,
`cd server && go test ./internal/protocol/...`, `make golden`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
