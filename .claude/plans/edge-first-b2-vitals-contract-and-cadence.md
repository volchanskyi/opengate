# EF-B2 — The vitals contract: extrema, 60 s cadence, the ≤ 24 cap, and a bounded dim vocabulary

**Master plan:** `edge-first-telemetry-and-investigations.md` §3.1, §6.2, §6.3 (A), §7.6 (I2, I5),
step 6 (code half).
**Acceptance criteria owned:** **B1**, **B4**.
**Dependencies:** EF-B1 (`disk.mounts_critical` must exist before the cap is counted).
**Blocks:** EF-B3 (measurement), EF-B4 (stall vitals), EF-B5 (disk performance).

## Context — verified

The dim vocabulary's single source of truth is
[store_sink.rs](../../agent/crates/mesh-agent-core/src/ml/store_sink.rs): `SERIES_*` ids,
`BACKFILL_SERIES`, and the paired `series_dim_name` / `dim_series` mappings that keep live
streaming and reconnect backfill in the *same* VM series.
[`HostMetricWindower`](../../agent/crates/mesh-agent-core/src/ml/host_metric_stream.rs) folds 1 Hz
samples into **10 s averages** today; `DIMS` is derived from `BACKFILL_SERIES.len()`, so the
contract grows from one place.

**Averaging is what destroys a stall, not the sample rate** (§2.5): inside a 60 s bucket at 1 Hz, a
5 s CPU-pinning freeze moves `avg` from 20 % to 26.7 % — noise — while `max` reads 100 %.

**A hole to close in the same step (I2/I5).** `handleAgentMetricWindow`
([conn_telemetry.go:68](../../server/internal/agentapi/conn_telemetry.go#L68)) copies
`msg.Dims[].Name` straight into the VM `dim` label with **no allowlist and no bound**. Central
cardinality is therefore agent-controlled today, which contradicts I2 outright ("bounded by a
compile-time constant") and is untrusted-input handling the rest of the ingest path already does
(WS-19 bounds the breach `metric` label; this path bounds nothing). A cap test that only counts what
a *well-behaved* agent sends would be measuring the wrong thing.

## The contract (fixed — do not re-derive)

**Platform-neutral (16):** `cpu.total`, `cpu.total.max`, `mem.used_percent`, `mem.used_percent.max`,
`disk.used_percent`, `disk.mounts_critical`, `net.rx_bps`, `net.rx_bps.max`, `net.tx_bps`,
`net.tx_bps.max` (10 dims of `opengate_edge_metric_avg`) + `node_anomaly_rate` (1) +
`family_anomaly_rate` × 5 fixed families (5).

**Linux adds 8** — 3 disk-performance (EF-B5) + 5 stall (EF-B4) — for **24**, the hard cap and
the count every device emits, since the agent implements Linux. **The headroom is spent**: the next
vital of any kind re-opens D3.

*Extension point:* a Windows agent would supply the 16 platform-neutral vitals plus whatever
Windows-native equivalents of the 8 it can name honestly — under their own names, never under
`stall.*` or `disk.await_ms` (D23).

**Cadence 60 s.** The 10 s central stream is removed; the edge keeps 1 Hz locally.

## File inventory

- **Modify:** [host_metric_stream.rs](../../agent/crates/mesh-agent-core/src/ml/host_metric_stream.rs)
  — 60 s bucket, per-bucket `max` beside the running sum.
- **Modify:** [store_sink.rs](../../agent/crates/mesh-agent-core/src/ml/store_sink.rs) — the four
  `.max` dims (see the trap on ids below).
- **Modify:** [backfill.rs](../../agent/crates/mesh-agent-core/src/ml/backfill.rs) — roll to the same
  60 s bucket as live, and carry `.max` from the tier's stored `max` rather than recomputing.
- **Modify:** [conn_telemetry.go](../../server/internal/agentapi/conn_telemetry.go) — dim allowlist +
  `unknown_dim` typed drop.
- **Modify:** [handlers_device_metrics.go](../../server/internal/api/handlers_device_metrics.go) —
  `minRangeStepSecs` 10 → 60.
- **Modify:** [api/openapi.yaml](../../api/openapi.yaml) — band enum `avg_of_10s` → `avg_of_60s`;
  regen Go + TS.
- **Modify:** [DeviceMetrics.tsx](../../web/src/features/devices/DeviceMetrics.tsx) — the band caption
  at [:34](../../web/src/features/devices/DeviceMetrics.tsx#L34) and the request at
  [:151](../../web/src/features/devices/DeviceMetrics.tsx#L151); plus
  [device-store.ts](../../web/src/features/devices/state/device-store.ts#L41).
- **Modify:** [spike_test.go](../../server/tests/vmcardinality/spike_test.go) — replace the
  40-series **projection** with the real 24-series contract.
- **Regenerate:** [testdata/golden/](../../testdata/golden/) metric-window fixtures.
- **Docs:** [Monitoring.md](../../docs/Monitoring.md), [Architecture.md](../../docs/Architecture.md),
  [API-Reference.md](../../docs/API-Reference.md).

## Steps (TDD-first)

1. **Test first (B4):** a 60-sample bucket where 5 consecutive samples read 100 % CPU and the rest
   read 20 % → `cpu.total` ≈ 26.7 and `cpu.total.max` == 100. This is the whole justification for
   extrema; it goes in before the code.
2. **Test first:** the windower closes on 60 s boundaries, emits nothing for a partial window, and
   two consecutive windows are stamped exactly 60 s apart (the existing 10 s boundary test is the
   template — change it, don't duplicate it).
3. **Test first:** `.max` dims round-trip through `series_dim_name`/`dim_series`, appear exactly once
   in `BACKFILL_SERIES`, and a live window equals a backfilled rollup of the same input for **every**
   dim including the new maxima.
4. **Test first (B1, server side):** a metric window carrying an unlisted dim name persists nothing
   for that dim and increments `opengate_edge_telemetry_drops_total{reason="unknown_dim"}`; a window
   carrying 1 000 junk dims creates **zero** new series. Then add the allowlist as a Go constant set
   derived from one place, with a unit test asserting `len(allowlist) + anomalySeries == 24`.
5. **Test first (B1, end-to-end):** in [vmcardinality](../../server/tests/vmcardinality/), write one
   device's **full Linux vitals set** against a real VictoriaMetrics and assert active series ≤ 24
   for that device, and that a 25th dim fails the assertion. Tag writes with a `run_id` — the suite
   shares one VM instance, so a TSDB-wide count is not device-scoped.
6. Implement the cadence change; delete nothing that the edge needs at 1 Hz — only the **central**
   stream cadence moves.
7. Rename the band enum and its caption; regen Go + TS. `avg_of_10s` is a user-visible string that
   becomes false at 60 s cadence, and there are no external API consumers.
8. Regenerate goldens; `make golden` green both directions.
9. Docs: the vitals table, the cap, the cadence, and what the band means now.

## Traps

- **`SeriesId`s are a persistence contract** (on-disk redb rows survive an agent upgrade). Append,
  never renumber.
- `.max` must come from the tier's **stored** `max` on the backfill path
  ([`StoredTierPoint`](../../agent/crates/edge-tsdb/src/tier.rs) already holds `min/max/sum/last/count`)
  — recomputing it from rolled averages would produce a max-of-averages, which is a different, wrong
  number that no test would notice unless the live/backfill equivalence test covers the maxima.
- **No percentiles.** `StoredTierPoint` has no percentile structure and p99 is dropped by decision
  (D7). Do not add a t-digest "while you're in here".
- `minRangeStepSecs` is referenced by `Downsampled: stepSecs > minRangeStepSecs`; changing it to 60
  changes that flag's meaning correctly, but check the web does not hard-code 10 anywhere.
- EF-A2 also touches `metrics_assemble.go` / `handlers_device_metrics.go`. Land EF-A2 first or
  expect a rebase; do not change the grid logic here.
- The total reaches 24 only after EF-B4 and EF-B5 land. Assert **≤ 24** here; the exact count is
  asserted by EF-B5 (the last of the two to land).

## Out of scope

The RAM-per-series measurement and any VM pod sizing — EF-B3, and the limit **stays at 512 Mi**
regardless of what this step observes.

## Reviewer checklist

- [ ] B4 fixture proves `max` recovers a 5 s stall that `avg` hides.
- [ ] 60 s cadence everywhere central; 1 Hz local sampling untouched.
- [ ] Unknown dims are dropped **and counted**; junk dims create no series.
- [ ] Cap test runs against real VictoriaMetrics, is device-scoped, and fails on a 25th series.
- [ ] Live/backfill equivalence extended to the maxima; goldens regenerated.
- [ ] Band enum + caption renamed; OpenAPI, Go, and TS regenerated together.

## Verification

`cd agent && cargo test -p mesh-agent-core`, `cd server && go test ./internal/agentapi/... ./internal/api/... ./tests/vmcardinality/...`,
`cd web && npm test`, `make golden`, `/precommit`.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
