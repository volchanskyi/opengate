# EF-A1 — Telemetry ingest accounting: typed drops, the I1 invariant, clock clamping

**Master plan:** `edge-first-telemetry-and-investigations.md` §6.1 (A1, A2, A4), §7.6 (I1, I5),
steps 1 and 3.
**Acceptance criteria owned:** **A1**, **A2**, **A4**.
**Dependencies:** none — ships first, alone.
**Blocks:** nothing structurally, but every later measurement (Q3, Q11, Q12) is untrustworthy until
this lands, because the pipeline currently loses data without saying so.

## Context — the defect, verified in the tree

`acceptTelemetry` increments the ingest counter
([conn_telemetry.go:185](../../../server/internal/agentapi/conn_telemetry.go#L185) →
`acceptedTelemetry` [:206](../../../server/internal/agentapi/conn_telemetry.go#L206)) **before** any
handler decides whether it has anything to persist. Two discard paths then return without touching
a counter:

| Path | Line | Silent because |
|---|---|---|
| `handleAgentHealthSummary` | [:55](../../../server/internal/agentapi/conn_telemetry.go#L55) | `if len(samples) == 0 { return nil }` |
| `bufferTelemetry` | [:161](../../../server/internal/agentapi/conn_telemetry.go#L161) | `len(samples) == 0` guard — reached from **all four** telemetry handlers |

`handleAgentMetricWindow` ([:62](../../../server/internal/agentapi/conn_telemetry.go#L62)) builds
`samples` from `msg.Dims`; an empty `Dims` yields an empty slice that dies in `bufferTelemetry`.
That is the live loss the master plan measures (M5 + M6 + M7 + M9): ~2 250 windows received,
counted, never written, never dropped.

Two more silent paths exist in the same package and are in scope here (the fix is worthless if the
next variant is just as invisible):

- `handleProcessReport` ([:80](../../../server/internal/agentapi/conn_telemetry.go#L80)) — an empty
  `TopN` reaches `bufferTelemetry` with nothing and persists nothing.
- `handleHealthWindowResponse` ([:126](../../../server/internal/agentapi/conn_telemetry.go#L126)) — an
  empty `Summaries` does the same.
- `handleMetricBackfillBatch` ([conn_backfill.go:67](../../../server/internal/agentapi/conn_backfill.go#L67))
  `continue`s every sample outside `[now-90 d, now+1 h]` with **no counter at all**.

Existing machinery to reuse — do not invent a parallel one: `dropTelemetry(reason, args…)`
([:237](../../../server/internal/agentapi/conn_telemetry.go#L237)) already feeds
`EdgeTelemetryDropsTotal` in [metrics.go](../../../server/internal/metrics/metrics.go) (namespace
`opengate`, so the series is `opengate_edge_telemetry_drops_total{reason}`). Reasons in use today:
`payload_too_large`, `interval_floor`, `tenant_missing`, `persist_failed`, `persist_slots_full`,
`tombstoned`, `discovery_payload_too_large`, `discovery_interval_floor`.

`telemetryTimestamp` ([:247](../../../server/internal/agentapi/conn_telemetry.go#L247)) trusts the
agent's clock completely — M10 measured a **+7 h** host-clock jump on the reference machine.

## File inventory

- **Modify:** [conn_telemetry.go](../../../server/internal/agentapi/conn_telemetry.go) — typed drops in
  all four handlers, clamping + named bounds in `telemetryTimestamp`.
- **Modify:** [conn_backfill.go](../../../server/internal/agentapi/conn_backfill.go) — count the
  out-of-retention sample skip.
- **Modify:** [metrics.go](../../../server/internal/metrics/metrics.go) — one **new counter** for clock
  clamping (see the trap below); no change to the ingest/drop counters.
- **Create:** `server/internal/agentapi/conn_accounting_test.go` — the I1 structural test.
- **Modify:** existing `conn_part*_test.go` where a handler's behaviour changes.
- **Docs:** [Monitoring.md](../../../docs/infrastructure/Monitoring.md) — the new drop reasons and the clamp counter.

## Steps (TDD-first)

1. **Test first:** a table-driven case per handler × empty-payload branch asserting
   `opengate_edge_telemetry_drops_total{reason}` advances by exactly 1 and no sample is written
   (fake `telemetry` writer + `prometheus/testutil`) → then route each branch through
   `dropTelemetry`. Reason vocabulary: `empty_dims` (metric window), `empty_summary` (health
   summary), `empty_processes` (process report), `empty_summaries` (health-window response),
   `backfill_out_of_retention` (per-sample skip, counted once per batch with the skipped count in
   the log args).
2. **Test first (this is the durable fix, A2):** `TestTelemetryAccountingInvariant` — drive **every
   branch of every counted-ingest message type** through a real `AgentConn` with fakes, then assert
   `ingested == persisted + Σ drops` at message granularity (`persisted` = messages that produced
   ≥ 1 write). Include a **guard case** that fails when a new telemetry message type is added to the
   dispatch switch without a row in the table — pin the enumerated list next to
   `protocol.Msg…` constants and assert the dispatch cases and the table agree. Without that guard
   this test rots the first time the protocol grows.
3. **Test first:** clamp cases — `now + 7 h` → clamped to `now + maxTelemetrySkew`; `now − 8 d` →
   clamped to `now − maxTelemetryBacklog`; `now − 6 d` → untouched; a batch of five out-of-order
   summaries → relative order preserved after clamping → then implement clamping in
   `telemetryTimestamp` with named constants and the reasoning in a doc comment (not literals):

   | Constant | Value | Reason (from §6.1 A4) |
   |---|---|---|
   | `maxTelemetryBacklog` | `7 * 24 * time.Hour` | Covers the longest reconnect backfill a bounded local queue can present; well inside VM's 30 d retention |
   | `maxTelemetrySkew` | `5 * time.Minute` | Larger than any NTP-correctable drift, smaller than 5 vitals buckets; the observed +7 h clamps hard |

4. **Test first:** the clamp counter increments with a `direction` label (`future` / `past`) and the
   message is **still persisted** → then add
   `opengate_edge_telemetry_clock_clamped_total{direction}` to `metrics.go`.
5. Docs: [Monitoring.md](../../../docs/infrastructure/Monitoring.md) gains the new reasons and the clamp counter.

## Traps

- **The clamp counter must NOT be a `dropTelemetry` reason.** A clamped message is still persisted;
  filing it under `…_drops_total` breaks the very identity step 2 asserts. The master plan names the
  signal `clock_skew_clamped` without naming the counter — it gets its own counter. If the reviewer
  disagrees, the invariant test is the arbiter, not preference.
- **Do not apply `telemetryTimestamp`'s clamp to `handleMetricBackfillBatch`.** That path has its own
  `backfillRetentionSecs = 90 * 24 * 3600` floor
  ([conn_backfill.go](../../../server/internal/agentapi/conn_backfill.go)); routing it through a 7 d
  clamp would silently truncate 90 d of reconnect backfill — the same class of defect this plan
  exists to remove. Two paths, two bounds, both explicit.
- Clamping is monotone, so ordering survives by construction; ties after clamping must keep input
  order (assert it — a `sort` introduced later would break it silently).
- `dropTelemetry` is called on the single read-loop goroutine; keep it that way (no new goroutine).

## Out of scope

Grid assembly (EF-A2), retention drift (EF-A2), anything about vitals content or cadence.

## Reviewer checklist

- [ ] Every `return`/`continue` that discards received telemetry increments exactly one typed drop.
- [ ] `TestTelemetryAccountingInvariant` drives every branch **and** fails on an unlisted message type.
- [ ] Clamp bounds are named constants carrying the §6.1 A4 reasoning; no literals at the call site.
- [ ] Clamped ≠ dropped: separate counter, message still persisted, intra-batch order preserved.
- [ ] Backfill's 90 d floor is untouched; its skip is now counted.
- [ ] `make lint`, `cd server && go test ./internal/agentapi/... ./internal/metrics/...`, coverage ≥ 80 %.

## Verification

`cd server && go test -race ./internal/agentapi/... ./internal/metrics/...`, then `/precommit`.
Post-deploy sanity: `opengate_edge_telemetry_drops_total` must have a **present, non-empty** series
set for the first time (M6 measured it absent) and `ingested − Σ drops` must track persisted writes.

## Close-out (mandatory)

In the commit that lands the implementation: `git mv` this plan to `archive/`, bump its internal
relative links one `../` deeper, and repoint the master plan's micro-plan index row.
