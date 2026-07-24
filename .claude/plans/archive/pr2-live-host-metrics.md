# PR-2 — Live host-metric streaming to central VictoriaMetrics (cumulative)

Micro-plan of [system-logs-and-central-host-metrics.md](system-logs-and-central-host-metrics.md).
Depends on **PR-1** (removes the conflicting `AgentMetricWindow` producer and
repoints the golden). Self-contained.

## Objective

Close the architectural gap: stream host metrics (`cpu.total`,
`mem.used_percent`, `disk.used_percent`, `net.rx_bytes`, `net.tx_bytes`) live to
central VM so the Telemetry pane charts them continuously — not only after a
reconnect-backfill. Reuse the existing `AgentMetricWindow` → `opengate_edge_metric_avg`
ingestion and the frontend family charts unchanged.

## Decisions carried in (locked)

- **Cumulative** net bytes (Q4/Option A) — emit exactly what reconnect-backfill
  writes; `net.*_bps` rate is a deferred follow-up.
- **60 s heartbeat drain** — windows queue on a bounded channel and flush on the
  existing 60 s control-loop heartbeat (points remain correctly 10 s-stamped in
  VM; delivery lag ≤ 60 s). Reuses the channel/drain pattern PR-1 removed.
- Emit from **inside the sampler** — it already computes the 1 s sample and is
  maintenance-gated; no second sampler.

## The invariant (must hold, tested)

Reconnect-backfill ships the **10 s average** of each stored series via
`roll_to_10s` (`sum/n`, keyed by `ts.div_euclid(10)*10`) —
[backfill.rs:375-386](../../../agent/crates/mesh-agent-core/src/ml/backfill.rs#L375-L386).
The live emitter must produce **identical** `(ts, value)` points for the same
1 s samples: aggregate into 10 s-aligned windows, stamp at the window **start**,
value = per-dim average. Then a gap-fill after reconnect is continuous with live.

## File inventory

| File | Change |
|---|---|
| `agent/crates/mesh-agent-core/src/ml/backfill.rs` (or a new `ml/window.rs` helper) | Expose the 10 s-bucketing so the live path and `roll_to_10s` share one implementation (or keep `roll_to_10s` and prove equivalence in test). Bucket key `ts.div_euclid(10)*10`, value `sum/n`. |
| `agent/crates/mesh-agent/src/edge_sentinel.rs` | Add a `host_metric_tx: Option<SyncSender<ControlMessage>>` to `spawn_sampler`. In the 1 s loop, fold each sample into the current 10 s-aligned window (per-dim sum+count for cpu/mem/disk/net); when a sample crosses into a new window, emit an `AgentMetricWindow{ts=window_start, dims=[cpu.total, mem.used_percent, disk.used_percent, net.rx_bytes, net.tx_bytes avgs]}` via `try_send` (drop on full), then reset. On maintenance enter/exit, **discard** the partial accumulator (never emit a window spanning maintenance). Dim labels come from `store_sink::series_dim_name`. |
| `agent/crates/mesh-agent/src/main.rs` | Add `HOST_METRIC_TELEMETRY_CAP` + the `host_metric_tx/host_metric_rx` bounded channel; pass `host_metric_tx` into `spawn_sampler`; drain `host_metric_rx` into the heartbeat `windows` Vec alongside discovery/health. |
| `server/internal/agentapi/conn_telemetry_test.go` (or `conn_metrics_test.go`) | Add a case asserting a host-dim `AgentMetricWindow` (e.g. `dim=cpu.total`) ingests to `opengate_edge_metric_avg`. Ingestion code is unchanged — this locks the contract. |
| `web/src/features/devices/DeviceMetrics.test.tsx` | Assert the pane renders `cpu`/`mem`/`disk`/`net` families from `opengate_edge_metric_avg` (guards the family path now that real host dims arrive). No source change expected. |

## TDD-ordered steps

1. **RED:** unit test in the agent that feeds a fixed 1 s sample sequence to the
   live 10 s aggregator and asserts the `(ts,value)` output **equals** `roll_to_10s`
   on the same input, for each of the five dims (incl. cumulative net). Fails —
   aggregator absent.
2. Implement the aggregator (shared helper or in-sampler) → GREEN.
3. Wire `spawn_sampler` emit + the `host_metric_tx/rx` channel + heartbeat drain.
4. Sampler tests: emits one window per 10 s boundary; **maintenance discards** the
   partial window (no emit while/around maintenance); channel-full drops silently.
5. Server test (host-dim ingestion) + web test (families render).
6. Verify end-to-end locally where possible (`make e2e` if the telemetry path is
   covered); otherwise confirm via the server/web tests.
7. `make test` / `make lint` / `make golden`; `/precommit` → commit → `/refactor`
   → `/precommit` → commit → push.
8. Archive this micro-plan in the final commit (link bump + `check-doc-links`).

## Edge / error cases

- **Server 10 s floor** ([conn_telemetry.go:157-174](../../../server/internal/agentapi/conn_telemetry.go#L157-L174)): windows are stamped 10 s apart (`ts = 10k`), so `ts-last = 10` is **not** `< 10` → accepted. Test a two-window emit is not floor-dropped.
- **Maintenance:** the sampler already `continue`s; additionally reset the accumulator so no partial/short window is emitted, and clear it on the maintenance→Active edge (alongside the existing re-baseline).
- **Net counter reset** (agent restart / NIC churn): the 10 s avg dips; inherent to a cumulative series and identical to backfill — no special handling.
- **Channel full:** `try_send` drops the window; never backpressure the control stream (same contract as discovery/health telemetry).
- **First/partial window at startup or disconnect:** only emit a window when it *closes* (a later-window sample arrives); never emit a partial.

## Out of scope

- `net.*_bps` throughput (deferred).
- System Logs, filters, UI panes (PR-3).
- Any change to server ingestion or the frontend chart engine (both already handle these dims).

## Reviewer checklist

- [ ] **Invariant test present and green:** live 10 s aggregation == `roll_to_10s` for all five dims.
- [ ] Emit is 10 s-aligned, stamped at window start; server floor not tripped (tested).
- [ ] Maintenance discards the partial window; verified by test.
- [ ] Bounded channel, drop-on-full; drained on the 60 s heartbeat.
- [ ] Dim labels come from `series_dim_name` (single SSOT with backfill).
- [ ] Telemetry pane shows live cpu/mem/disk/net (manual/e2e); the `log`-family guard still present (defensive).
- [ ] Rust mutation score held on changed modules; `/precommit` green.

## DoD

`/precommit` green, `/refactor`, pushed to `dev`, micro-plan archived. No phases.md
row here — the workstream Completed row + master-plan archive land in PR-3.
