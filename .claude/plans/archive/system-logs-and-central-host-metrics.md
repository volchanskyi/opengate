# Master Plan / Spec — System Logs pane + central host-metric streaming

**Status:** Draft spec for approval (no source touched). On approval, this master
plan is decomposed into the per-PR micro-plans listed in §12.
**Author:** Ivan Volchanskyi · **Date:** 2026-07-23

---

## 1. Problem statement

Two coupled problems, one root cause.

1. **Architectural gap (must fix first).** The host system-resource samples
   (`cpu_total_percent`, `memory_used_percent`, `disk_used_percent`,
   `network_rx_bytes`, `network_tx_bytes`) are sampled every 1 s, fed to the
   local anomaly ensemble, and written to the agent-local redb TSDB — but they
   are **never live-streamed to central VictoriaMetrics**. They reach VM only via
   reconnect-backfill (gap-fill) or on-demand `GetDeviceHistory` (per-dim pull).
   In steady state the "Telemetry" pane has no live cpu/mem/disk/net to chart.

2. **Feature.** The "View logs for this window" button on the Telemetry / edge
   health pane must point to **system (host) logs**, shown in a **new pane**
   modelled on the existing Agent Logs pane.

Root cause link: the **only** live emitter of the charted series
`opengate_edge_metric_avg{dim=…}` today is the **host log-rate reader**, whose
`log.rate.*` dims the Telemetry pane deliberately filters out. That log-rate
feature is unused and is being **deleted**, which simultaneously (a) frees the
`opengate_edge_metric_avg` channel for real host metrics and (b) frees the
host-log **collectors** (journald / Windows Event Log) to power the new pane.

---

## 2. Current-state findings (empirical, with proofs)

| # | Fact | Proof |
|---|------|-------|
| F1 | `opengate_edge_metric_avg{dim}` is the charted metric; its **only** live producer is the log-rate window. | [conn_telemetry.go:59-77](../../../server/internal/agentapi/conn_telemetry.go#L59-L77) ← [host_logs.rs `build_log_rate_window`](../../../agent/crates/mesh-agent/src/host_logs.rs#L216-L230) |
| F2 | cpu/mem/disk/net are written **only** to local redb; never emitted as a live `AgentMetricWindow`. | [edge_sentinel.rs:434-443](../../../agent/crates/mesh-agent/src/edge_sentinel.rs#L434-L443), [store_sink.rs:114-135](../../../agent/crates/mesh-agent-core/src/ml/store_sink.rs#L114-L135) |
| F3 | They reach VM only via reconnect-backfill (`MetricBackfillBatch`, 10 s tier) or on-demand `GetDeviceHistory`. | [backfill_loop.rs](../../../agent/crates/mesh-agent/src/backfill_loop.rs), [conn_backfill.go:44-90](../../../server/internal/agentapi/conn_backfill.go#L44-L90), [handlers_device_history.go:22-67](../../../server/internal/api/handlers_device_history.go#L22-L67) |
| F4 | Backfill ships the **10 s average** of the stored series (`sum/n`). | [backfill.rs `roll_to_10s`:375-386](../../../agent/crates/mesh-agent-core/src/ml/backfill.rs#L375-L386) |
| F5 | The web pane was **built** for `cpu.*/mem.*/disk.*/net.*` families, special-casing `cpu.total`, and **filters out** the `log` family. | [aligned-data.ts:41-61,143-173](../../../web/src/features/devices/charts/aligned-data.ts#L143-L173), [DeviceMetrics.tsx:187-201](../../../web/src/features/devices/DeviceMetrics.tsx#L187-L201) |
| F6 | The dim ↔ series map (`cpu.total`, `mem.used_percent`, `disk.used_percent`, `net.rx_bytes`, `net.tx_bytes`) is the shared SSOT for live + backfill. | [store_sink.rs:39-76](../../../agent/crates/mesh-agent-core/src/ml/store_sink.rs#L39-L76) |
| F7 | `RequestDeviceLogs` **already** carries `source`("self"/"journald"/"windows") + `unit`, but the agent ignores them and always reads its own files. | Rust [control.rs:390-411](../../../agent/crates/mesh-protocol/src/control.rs#L390-L411), Go [control.go:223-226](../../../server/internal/protocol/control.go#L223-L226), [main.rs:823-826](../../../agent/crates/mesh-agent/src/main.rs#L823-L826) |
| F8 | Each `LogEntry` already carries the emitting unit in `target` (journald `_SYSTEMD_UNIT`/`SYSLOG_IDENTIFIER`; Windows `ProviderName`) and a normalized `level`. | [host_logs.rs:73-115](../../../agent/crates/mesh-agent/src/host_logs.rs#L73-L115) |
| F9 | Log filtering (`matches_filter`: severity `level`, time window, `search`) lives **only** in the agent-self `LogCollector` path; `collect_host_logs` returns **unfiltered** recent entries. | [logs.rs:222-260](../../../agent/crates/mesh-agent/src/logs.rs#L222-L260), [host_logs.rs:248-297](../../../agent/crates/mesh-agent/src/host_logs.rs#L248-L297) |
| F10 | Log-rate is **telemetry-only** — no detector consumes it (ensemble is `EdgeMlEnsemble<3>` over `[cpu,mem,disk]`). | [edge_sentinel.rs:340-366](../../../agent/crates/mesh-agent/src/edge_sentinel.rs#L340-L366) |
| F11 | After `spawn_log_readers` is removed, `collect_host_logs` + `LogSource::AgentSelf` have **no non-test caller**; Agent Logs uses `LogCollector` directly. | [main.rs:829](../../../agent/crates/mesh-agent/src/main.rs#L829), grep in §11 |
| F12 | Server enforces a **10 s per-message-type ingest floor** on telemetry. | [conn_telemetry.go:14-19,157-174](../../../server/internal/agentapi/conn_telemetry.go#L157-L174) |

---

## 3. Decisions (locked)

| Q | Decision |
|---|----------|
| Q1 — System-log source model | **Auto-resolve `source=host`.** Agent picks journald (Linux) / Windows Event Log (Windows); returns empty where neither exists. No OS logic in the browser. |
| Q2 — "View logs for this window" | **Repoint to the System Logs pane.** Agent Logs stays browsable, loses the correlation jump. |
| Q3 — Unit filter | **Include now**, agent-side, **with auto-detected units selectable from a UI dropdown** (not free text). Render the `target` column + click-to-filter. |
| Q4 — Network metric | **Option A:** stream cumulative `net.*_bytes` exactly as reconnect-backfill already writes them (live == backfill, zero divergence). **Per-interval rate (`net.*_bps`) is deferred** to a later, coherent change across both paths. |
| + | Delete the log-rate feature entirely; **clean up `LogSource::AgentSelf`** (dead after deletion). |
| + | System Logs pane carries **level** + search + window + unit filters (level semantics identical to Agent Logs). |
| + | "Refresh Hardware" and Discovered-Footprint "Refresh" buttons become **icon-only**. |
| + | **Units delivery:** `available_units` is **bundled in the host-log response** (one round-trip, always fresh). |
| + | **Logs layout:** **two separate cards** (Agent Logs, System Logs); the drill-down focuses the System Logs card. |
| + | **Packaging:** the two icon-button changes ship **inside the System Logs PR** (single web PR). |

---

## 4. Scope

**In scope**
- Delete host log-rate collection/telemetry and its dead `AgentSelf` path.
- Live-stream host metrics (cpu/mem/disk/net, cumulative) to VM via the existing
  `AgentMetricWindow` → `opengate_edge_metric_avg` path, at the 10 s cadence.
- New **System Logs** pane (`source=host`) with **level** + search + window +
  **unit** filters, a `target` column, and auto-detected selectable units.
- Repoint the Telemetry drill-down to System Logs.
- Icon-only refresh buttons (hardware, footprint).
- Remove the **Recent Activity** pane from the Dashboard (main page).

**Out of scope** (propose separately if a need surfaces)
- **Per-interval network rate `net.*_bps`** — deferred; cumulative bytes stream
  now (matches backfill). Rate is a later change across live + backfill +
  frontend + goldens under a new dim name.
- Rewriting cumulative `net.*_bytes` in the local store / redb schema.
- Central retention/deletion of stale `log.rate.*` series (retention ages them
  out; no operational script — see [editing-and-scope.md](../../rules/editing-and-scope.md)).
- Per-process telemetry changes (`handleProcessReport` unchanged).
- Any new central log **storage** — raw host lines stay transient/edge-first.

---

## 5. Domain entities & relationships

- **HostMetricSample** (`MetricSample`, [sampler.rs:20-35](../../../agent/crates/mesh-agent-core/src/ml/sampler.rs#L20-L35)) — 1 s snapshot: cpu%, mem%, disk%, cumulative rx/tx bytes, top processes.
- **MetricSeries / dim** — stable central label per series ([store_sink.rs:39-76](../../../agent/crates/mesh-agent-core/src/ml/store_sink.rs#L39-L76)): `cpu.total`, `mem.used_percent`, `disk.used_percent`, `net.rx_bytes`, `net.tx_bytes`.
- **AgentMetricWindow** (control msg) — `{ts, dims:[{name,avg}]}` → `opengate_edge_metric_avg{dim}` in VM ([conn_telemetry.go:59-77](../../../server/internal/agentapi/conn_telemetry.go#L59-L77)).
- **LogSource** — reduces to `Journald | WindowsEventLog`. `source=host` resolves per-platform.
- **LogEntry** — `{timestamp, level, target(unit), message}`; `target` is the unit/provider, `level` normalized (F8).
- **LogFilter** — `{level, from, to, search, offset, limit, source, unit}` (Go gains `source`/`unit`); severity + time + search semantics shared across sources.
- **LogExplorer (UI)** — shared component; two instances: **Agent Logs** (`source=self`, own files) and **System Logs** (`source=host`, unit filter).
- **AvailableUnits** — enumerated distinct units/providers for the resolved host source, surfaced to the UI dropdown.

Relationships: `HostMetricSample —(10 s avg)→ AgentMetricWindow —→ opengate_edge_metric_avg —(GetDeviceMetrics)→ family charts`. `RequestDeviceLogs{source,unit,level,from,to,search} —→ collect_host_logs(resolved)+filter —→ DeviceLogsResponse{entries, available_units}`.

---

## 6. Target architecture

### 6A. Central host-metric streaming (the gap fix)
- A **10 s host-metric emitter** aggregates the sampler's 1 s samples into one
  `AgentMetricWindow` (avg over the 10 s window) and sends it over a bounded
  channel drained on the control-loop heartbeat — the **same pattern** as the
  removed log-rate reader, on the **same message type**, reusing server
  ingestion (F1) and the frontend family charts (F5) unchanged.
- **Emit from inside the sampler task** ([edge_sentinel.rs `spawn_sampler`](../../../agent/crates/mesh-agent/src/edge_sentinel.rs#L273-L446)): it already computes the 1 s sample and is already maintenance-gated — accumulate 10 samples, emit the avg, reset. No second sampler, no double cost.
- **Invariant (F4/F6):** live avg-over-10 s is byte-identical to backfill
  `roll_to_10s` (`sum/n`). A cross-path test asserts identical aggregation.
- Dims: `cpu.total`, `mem.used_percent`, `disk.used_percent`, `net.rx_bytes`,
  `net.tx_bytes` — **cumulative** bytes exactly as backfill writes them (Q4/A).
  A network chart is a rising cumulative line; a counter reset (agent restart /
  NIC churn) renders as a sawtooth dip in the avg/min/max band — inherent to a
  cumulative series and identical to backfill. Throughput (`net.*_bps`) is a
  deferred follow-up (§4).

### 6B. Delete log-rate + clean up `AgentSelf`
- Delete `ml/log_rate.rs` and the rate-folding in `host_logs.rs`
  (`log_rate_vector`, `log_rate_dims`, `build_log_rate_window`,
  `LOG_RATE_FIELD_LABELS`, `source_label`, the `LogRateExtractor`/`MetricDim`
  imports). **Keep only** `redact_entries` (+ helpers), still used by the Agent
  Logs path. The journald/Windows collectors + parsers + `LogSource` are orphaned
  once `spawn_log_readers` is gone and would fail `clippy -D warnings` (dead
  code), so PR-1 **deletes** them; PR-3 **rebuilds** host collection purpose-built
  (raw entries + level/time/unit filtering + unit enumeration), reusing the
  parsing logic from git history.
- Delete `spawn_log_readers` + `LOG_READER_INTERVAL` + `LOG_SOURCES`
  ([edge_sentinel.rs:22-65](../../../agent/crates/mesh-agent/src/edge_sentinel.rs#L22-L65)); the log-rate channel + drain + `LOG_RATE_TELEMETRY_CAP` in main.rs (F1/§11); the `bench_log_rate_window_fold` bench.
- **Remove `LogSource::AgentSelf`** (F11) — enum becomes `Journald |
  WindowsEventLog`; `source="self"`/`""` continues to route to `LogCollector`
  in the `RequestDeviceLogs` handler (Agent Logs unaffected). `make dead-code`
  polices leftovers.
- Comments narrating log-rate (cli_flags.rs:3, maintenance.rs:5-6) rewritten to
  **current** state per [docs-live-state.md](../../rules/docs-live-state.md).
- Keep the Telemetry pane's family guard, generalized to "chart only
  system-resource families," so any `log.rate.*` still inside VM retention never
  renders a stray chart during the overlap.

### 6C. System Logs pane (`source=host`) + level + unit filters
- **Protocol threading:** add `Source`/`Unit` to Go `device.LogFilter`
  ([device.go:106-113](../../../server/internal/device/device.go#L106-L113)) and set
  them in `SendRequestDeviceLogs` ([conn.go:309-324](../../../server/internal/agentapi/conn.go#L309-L324)).
  The Rust/Go `ControlMessage` fields already exist (F7).
- **Agent dispatch:** rebuild the host collectors (deleted in PR-1) purpose-built,
  then branch the `RequestDeviceLogs` handler on `source`: `""`/`"self"` →
  `LogCollector` (own files, unchanged); `"host"` → resolve platform →
  `collect_host_logs(Journald|WindowsEventLog)`.
- **Level + time + search on host logs (the substantive add).** `collect_host_logs`
  currently returns unfiltered entries (F9). The host path applies the **shared
  severity/time/search filter** so level semantics match Agent Logs exactly
  (min-severity: WARN ⊇ ERROR). Reuse the `matches_filter` logic (lift it to
  operate on `&[LogEntry]`) rather than duplicate it. Push down where cheap to
  bound reads: journald `-p <level>` + `--since/--until`; Windows
  `-FilterHashtable @{Level;StartTime;EndTime}` — but always apply the shared
  filter client-side too (uniform semantics, defense in depth).
- **Agent-side unit filter:** when `unit` set, pass it to the collector as a
  **discrete argument** — journald `journalctl _SYSTEMD_UNIT=<unit>` (argv, no
  shell → injection-safe); Windows `Get-WinEvent -FilterHashtable @{ProviderName=…}`
  built through an **allowlist** (`[A-Za-z0-9._ -]`, reject otherwise) because
  that path composes a PowerShell `-Command` string ([host_logs.rs:283-297](../../../agent/crates/mesh-agent/src/host_logs.rs#L283-L297)). Both get an injection fixture test.
- **Auto-detected units (Q3):** agent enumerates distinct units for the resolved
  source — journald `journalctl -F _SYSTEMD_UNIT` (indexed field enum; cheap),
  Windows provider list — capped + sorted, returned in a new
  `available_units: []string` on `DeviceLogsResponse` (`#[serde(default)]`,
  empty for `self`). Enumeration is **bundled in the response** — one
  round-trip, always fresh, no separate endpoint.
- **OpenAPI:** add `source` (enum `self|host`) + `unit` query params to
  `/devices/{id}/logs`; add `available_units` to `DeviceLogsResponse`;
  regenerate Go (`oapi-codegen`) + TS (`npm run generate:api`).
- **Web:** refactor `DeviceLogs.tsx` into a reusable `<LogExplorer>` (table +
  **level** (severity dropdown + facets) / search / window (15m/1h/6h/24h)
  filters + pagination), then two thin wrappers: **AgentLogs** (`source=self`)
  and **SystemLogs** (`source=host`, `showUnitFilter`, unit dropdown from
  `available_units`, `target` column, click-to-filter). The two render as
  **separate cards** in DeviceDetail (not tabs). System Logs opens **unbounded
  (most-recent-N)**, mirroring Agent Logs — no default window. Store gains a
  **source-keyed** logs slice so the two panes hold independent state.
- **Repoint drill (Q2):** `DeviceDetail.onViewLogs` sets the **System Logs**
  focus window instead of Agent Logs ([DeviceDetail.tsx:475-502](../../../web/src/features/devices/DeviceDetail.tsx#L475-L502)).
- **Remove Recent Activity:** delete the Recent Activity section from
  [Dashboard.tsx:92](../../../web/src/features/dashboard/Dashboard.tsx#L92) plus its
  now-unused events fetch/state and tests. Bundled here per the packaging
  decision; restore web mutation ≥ 85% in this PR.

### 6D. Icon-only refresh buttons
- Add a `RefreshIcon` to [icons.tsx](../../../web/src/components/icons.tsx) (only
  Play/Restart/Check/Trash/Wrench/Activity/Spinner exist today).
- "Refresh Hardware" ([DeviceDetail.tsx:435](../../../web/src/features/devices/DeviceDetail.tsx#L435)) and footprint "Refresh"/"Refreshing…" ([DeviceInventory.tsx:174-182](../../../web/src/features/devices/DeviceInventory.tsx#L174-L182)) become icon buttons with `aria-label`+`title`, `SpinnerIcon` while loading — matching the icon-only device-actions pattern already established (commit d077890). Update tests that query "Refresh Hardware"/"Refresh" text to query by accessible name. **Ships inside PR-3** (the System Logs web PR).

---

## 7. Component & dependency map

| Layer | Files touched (indicative) | PR |
|---|---|---|
| Rust agent-core | `ml/log_rate.rs` (del), `ml/mod.rs`, `ml/sampler.rs` (window-avg helper), `maintenance.rs` (comment), `benches/edge_sentinel_bench.rs` | 1,2 |
| Rust agent bin | `host_logs.rs` (del rate fns, drop AgentSelf, add level/time/unit filter + unit enum), `edge_sentinel.rs` (del readers, add host-metric emitter), `main.rs` (swap channels; dispatch logs by source), `tests/cli_flags.rs` (comment) | 1,2,3 |
| Wire protocol | Rust `control.rs` (source/unit exist; `available_units`), Go `control.go` (Source/Unit exist; `AvailableUnits`), golden fixtures | 1,3 |
| Go server | `agentapi/conn_telemetry.go` (reuse), `conn.go`+`device.LogFilter` (source/unit), `handlers_device_inventory.go` (logs params + available_units), `api/openapi.yaml` (+gen) | 3 |
| Web | `charts/aligned-data.ts`/`DeviceMetrics.tsx` (family guard), `LogExplorer`+`AgentLogs`+`SystemLogs`, `state/device-store.ts` (source-keyed logs), `DeviceDetail.tsx` (repoint + hw icon), `DeviceInventory.tsx` (icon), `icons.tsx` (RefreshIcon), `types/api.d.ts` (gen) | 2,3 |
| Loadtest/golden | `server/tests/loadtest/soak_telemetry.go` etc. (log.rate → host dims), golden `_test.go` + `testdata/golden/*` | 1,2,3 |

---

## 8. Non-functional requirements

- **Performance.** Host-metric emit reuses the existing 1 s sample (no new
  sampling); one bounded channel + one `AgentMetricWindow` per 10 s — strictly
  less traffic than today's 3-source/60 s log-rate readers. Host-log level/time
  pushed to journald `-p/--since` and the Windows `FilterHashtable` bounds reads;
  unit enumeration via journald `-F` is indexed/cheap (D1 caching option for
  Windows if profiling shows cost). Deleting the per-60 s 3-source log scan+fold
  is a **net CPU/IO reduction**.
- **Security.** Raw host lines keep **both** redaction layers (edge
  `redact_entries` + server `log_redact`). Unit filter is argv on journald and
  **allowlist-guarded** on the Windows `-Command` path (injection fixture test).
  Logs remain **transient** — never persisted centrally (F1 unchanged). Access
  stays admin-gated + group-scoped ([handlers_device_inventory.go:82-119](../../../server/internal/api/handlers_device_inventory.go#L82-L119)).
  No new central log storage → no new PII surface.
- **Maintainability.** Reuse over addition: same `AgentMetricWindow`/ingestion,
  same family-chart engine, one shared `<LogExplorer>` for both panes, one
  dim-map SSOT for live+backfill, one shared `matches_filter` for all sources.
  Deletion shrinks the surface (log-rate module, bench, an enum arm). Cumulative
  net avoids a store/schema migration; rate stays a clean future change.

---

## 9. Quality metrics · DoD · acceptance criteria

**Definition of Done (every PR):** `/precommit` gauntlet green (lint, tests,
coverage, golden, sonar, dead-code, shell, e2e where touched); `/refactor`; docs
updated where behavior changed; plan archived on the final PR (§12). TDD order
observed (failing test first).

**Mutation floor — same PR, non-negotiable.** Every PR lands its Stryker /
`cargo-mutants` / `gremlins` **killer tests in that same PR** so the nightly
**Mutation Testing** baseline-regression gate stays green — never deferred to a
follow-up. The **web** floor is **≥ 85%**; it currently sits at **84.6% on
`main`** (dropped from 85.4% by recent web work shipped without same-PR killers —
the exact failure mode this rule prevents). PR-3 (the web-heavy PR) must land web
**≥ 85%**, covering both its new code and this outstanding deficit. Rust
(~86%) and Go (~85.5%) scores on changed modules are held the same way.

**Acceptance criteria**
1. With an agent connected and idle-to-busy, the Telemetry pane shows **live**
   cpu/mem/disk/net updating on the 30 s poll (no reconnect needed). *(closes the gap)*
2. `opengate_edge_metric_avg{dim=cpu.total}` (and mem/disk/net) is present in VM
   for a **continuously-connected** device within ~1 min of connect.
3. Live and backfilled points for the same `(device,dim,ts)` are **equal** —
   asserted by a cross-path unit test (cumulative, all five dims incl. net).
4. No `log.rate.*` is emitted anywhere; `make dead-code` reports no leftover
   log-rate or `AgentSelf` code; golden suite has no log-rate fixture.
5. A **System Logs** pane shows journald (Linux) / Windows Event Log entries via
   `source=host`, with working **level** + search + **unit** + window filters,
   a `target` column, and a unit dropdown populated from `available_units`.
6. System Logs **level** filtering matches Agent Logs semantics (min-severity:
   selecting WARN returns WARN+ERROR) — asserted against a host-source fixture.
7. "View logs for this window" focuses the **System Logs** pane on the selected
   window and fetches it.
8. Agent Logs pane unchanged (15m/1h/6h/24h + level filters preserved).
9. A malicious `unit` string cannot inject a command on either collector
   (fixture test proves inert).
10. "Refresh Hardware" and footprint "Refresh" are icon-only with accessible
    names; loading shows the spinner.

---

## 10. Edge & error cases

- **Neither host log source present** (container/minimal host): `collect_host_logs`
  returns empty; pane shows "No logs available"; `available_units` empty →
  dropdown shows "All units" only. No error.
- **Net counter reset** (cumulative): a reset mid-window lowers the 10 s avg →
  a dip; inherent to a cumulative series and identical to backfill (no special
  handling under Q4/A).
- **10 s ingest floor (F12):** emitter cadence = 10 s exactly; a late tick must
  not emit two windows inside 10 s (would be dropped). Test the cadence.
- **Maintenance mode:** sampler (hence host-metric emit) already suppressed; no
  windows emitted while in maintenance — assert.
- **Bounded channel full:** drop the window (never backpressure control), same as
  today's telemetry.
- **`log.rate.*` still in VM retention:** family guard hides it (6B).
- **Unit dropdown:** bundled per-response, so always fresh (no cache staleness).
- **Large journald/provider set:** cap `available_units` (e.g. 200, sorted);
  unit filter still accepts an exact value not in the capped list.
- **Windows provider with spaces** (provider names contain spaces): allowlist
  must permit space; still bound as a single `FilterHashtable` value, never
  interpolated raw.
- **Level filter on a source with sparse severities:** journald `-p` maps
  min-severity correctly; Windows Level mapping is inverse (1 Critical..5 Verbose)
  — the shared filter normalizes both to the same severity scale.

---

## 11. Deletion surface (precise — for the "clean up lograte self" step)

Delete / edit (non-test unless noted):
- `agent/crates/mesh-agent-core/src/ml/log_rate.rs` — **file deleted**.
- `ml/mod.rs:9` — drop `pub mod log_rate;`.
- `host_logs.rs` — reduce to `redact_entries` (+ helpers/tests) **only**: drop the
  rate-folding (`log_rate_vector`, `log_rate_dims`, `build_log_rate_window`,
  `LOG_RATE_FIELD_LABELS`, `source_label`, the `LogRateExtractor`/`LOG_RATE_DIMS`/`MetricDim`
  imports) **and** the now-orphaned host collectors (`collect_host_logs`,
  `collect_journald`, `collect_windows_events`, `LogSource`, the journald/Windows
  JSON parsers + level/timestamp mappers) with their tests. PR-3 rebuilds host
  collection purpose-built.
- `edge_sentinel.rs:9,22,26-65` — drop the log-rate imports, `LOG_READER_INTERVAL`,
  `LOG_SOURCES`, `spawn_log_readers`.
- `main.rs:269,491-494,701` — drop `LOG_RATE_TELEMETRY_CAP`, the `log_rate_tx/rx`
  channel, the `spawn_log_readers` call, and the log-rate drain (replaced by the
  host-metric emitter's channel in PR-2).
- `benches/edge_sentinel_bench.rs:4,42-53,125,133` — drop `bench_log_rate_window_fold`
  + `LogRateExtractor` import + group entries.
- `cli_flags.rs:3`, `maintenance.rs:5-6` — rewrite comments to current state.
- Server: `golden_part2_test.go:21` row, `golden_part7_test.go:93-114` test,
  `testdata/golden/control_agent_metric_window_log_rates.{bin,meta.json}`.
- Loadtest: `soak_telemetry.go` (+ siblings) — `log.rate.*` → host-metric dims.
- Web: audit `DeviceLogs.test.tsx` log-rate reference; keep the generalized
  family guard in `DeviceMetrics.tsx`.

---

## 12. Sequenced micro-plans (each a standalone PR)

Ordered so the shared `AgentMetricWindow` throttle (F12) never has two producers,
and risk lands incrementally.

1. **PR-1 — Delete log-rate + orphaned host collectors + `AgentSelf`; repoint the
   `AgentMetricWindow` golden + loadtest to host-metric dims.** Removal + fixture
   repoint (6B, §11); keeps only `redact_entries`. Removes the only current
   `AgentMetricWindow` producer. Green everywhere.
2. **PR-2 — Live host-metric streaming (cumulative).** Host-metric emitter (6A)
   aggregating 1 s→10 s avg, drained on the existing **60 s heartbeat**; family
   charts light up live. Closes the gap; invariant test that the live 10 s avg
   equals backfill `roll_to_10s`.
3. **PR-3 — System Logs pane + level/unit filters + repoint drill + icon buttons
   + remove Recent Activity.** Protocol/OpenAPI threading, **rebuild host
   collectors** (purpose-built) + shared level/time/search filter on host logs +
   unit filter + `available_units` enumeration (bundled), `<LogExplorer>` refactor
   + a separate SystemLogs card, store source-keying, drill repoint, the two
   icon-only refresh buttons, and the Recent Activity removal (6C/6D). Injection
   fixture. **Lands web mutation ≥ 85% in this PR** (restoring the current 84.6%
   deficit) with Stryker killer tests included here. **Archive this plan (and the
   three micro-plans) in this PR** (§ plans-archive rule).

Per-PR micro-plans (file inventory, TDD step list, reviewer checklist) are
produced on approval of this master plan, per the master-plan→micro-plan flow.

---

## 13. Sub-decisions — resolved

- **Unit enumeration:** `available_units` **bundled in the host-log response**
  (one round-trip, always fresh, `#[serde(default)]` back-compat).
- **Logs layout:** **two separate cards** (Agent Logs, System Logs); the
  correlation drill focuses the System Logs card.
- **Icon buttons:** **folded into PR-3** (single web PR).
