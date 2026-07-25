# Device Detail & Dashboard UX + Telemetry Reliability

**Status:** Planning (awaiting go-ahead to implement)
**Rollout:** One combined plan / one branch / one PR (user decision)
**Author:** Ivan Volchanskyi

A single workstream that (a) makes the Dashboard and Fleet-Health cards navigable
into filtered device lists, (b) reworks the logs and telemetry panes' UX, (c)
corrects the network telemetry metric, and (d) fixes the Edge-health "No data"
gap. Built test-first; lands behind the full `/precommit` gauntlet.

---

## Locked decisions

| Topic | Decision |
|---|---|
| Network metric (#9/net) | **Per-second rate (B/s) on the auto-detected primary (default-route) interface.** Agent computes the rate; no server `rate()`. |
| Drag-to-correlate (#7) | **Freeze the 30 s auto-refresh while a selection is active.** Full preset window stays visible with the highlight + correlation; a ✕/Clear resumes live polling. |
| Edge-health "No data" (#11) | **Live investigation COMPLETE — bare-lookback fix refuted.** Root cause: server-side `persist_slots_full` starvation drops ~1271/1274 accepted anomaly summaries (host-metric firehose wins the 4-slot race; anomaly summary is tail-of-batch). **Fix LOCKED: E1 (coalesce persistence per connection) + E4 (`last_over_time([10m])` safety net).** Stays in the combined PR, diff isolated to `conn_telemetry.go`. |
| Rollout | One combined plan/PR. |

## Defaults (proceeding unless redirected)

- **Offline card/filter** = `status !== 'online'` (matches the Dashboard "Offline" count, which is `total − online` and includes `connecting`). *(confirmed)*
- **Device Groups card** → **deleted entirely** from the Dashboard; remove the now-unused `groups`/`fetchGroups` wiring in `Dashboard.tsx`. *(confirmed)*
- **Units from start (#10)** → System Logs pane auto-loads its most-recent default window on mount (populates `available_units` + shows recent logs). *(confirmed)*
- **Band caption (#8)** → remove the inline caption text; keep the band *rendering*. *(confirmed)*
- **Legend "Time --" (#8)** → disable uPlot's default legend (`legend:{show:false}`); the current value is already shown beside each family title.

---

## Scope — 14 items

Navigation/filter: **1** Dashboard cards clickable · **2** Fleet-Health cards clickable.
Logs UX: **3** resizable cards + collapsible output · **4** prev/next arrow pagination · **10** units from start.
Telemetry UI: **6** Start-Session green · **7** drag freeze · **8** remove band caption + "Time --" · **14** chart clips the data line at top/bottom.
Cosmetic: **5** recolor "Load More" + "View logs for this window" to Restart-Agent yellow.
Correctness: **9** network metric → primary-interface rate (cpu/mem/disk already correct — no change).
Reliability: **11** Edge-health "No data" server lookback fix (+ live investigation).
Trivial: **12** Group ID all-zeros UUID → "N/A" · **13** Hardware section collapsible (like Intel AMT Setup).

Out of scope: operational scripts, alerting, server-side device pagination, log-storage changes.

---

## Workstream A — Dashboard & Fleet-Health filtering (items 1, 2)

**Files:** `web/src/features/devices/DeviceList.tsx`, `web/src/features/dashboard/Dashboard.tsx`, `web/src/features/devices/FleetHealth.tsx`, new `web/src/features/devices/device-filter.ts` (+ `.test.ts`), `web/src/features/devices/FleetHealth.test.tsx`, `web/src/features/dashboard/Dashboard.test.tsx`, `web/src/features/devices/DeviceList.test.tsx`.

**Design:** a pure `applyDeviceFilter(devices, {status?, maintenance?, health?})` reducer in `device-filter.ts`. `DeviceList` reads `useSearchParams()` (react-router 7.18), applies the reducer alongside the existing `searchQuery`, and renders a removable filter chip (reuse the window-chip pattern from `LogExplorer`). `StatCard` already accepts `to` — point each card at a URL; make Fleet-Health cards `<Link>`s.

Card → URL (after deleting the Device Groups card):
- Total Devices → `/devices`; Online → `/devices?status=online`; Offline → `/devices?status=offline` (offline := `status !== 'online'`, includes `connecting`); In Maintenance → `/devices?maintenance=true`.
- Fleet-Health: anomalous/watch/healthy/`unknown` → `/devices?health=<band>` (band derived via `healthBand` — reuse `health.ts`).

**Delete Device Groups card:** remove the `StatCard` (`Dashboard.tsx:66-67`) plus the now-unused `groups`/`fetchGroups` selectors and the `fireAndForget(fetchGroups())` call (FleetHealth does not need groups). Adjust the grid from `lg:grid-cols-5` to `lg:grid-cols-4` for the remaining four cards. Update `Dashboard.test.tsx` to assert the card is gone and `fetchGroups` is no longer called.

**Edge cases:** unknown/garbage param → ignored (no filter); filter + group selection compose (filter narrows within the selected group); empty result → existing empty-state copy adjusted to mention the active filter; chip clear resets the param via `setSearchParams`.

**TDD:** reducer table tests (each status/maintenance/health + combinations + invalid) FIRST; then `DeviceList` renders the chip and filters; Dashboard/FleetHealth cards render correct `href`s.

**DoD:** clicking any Dashboard or Fleet-Health card lands on `/devices` showing exactly the matching devices with a clearable chip; deep-linking the URL reproduces the filter.

---

## Workstream B — Logs UX (items 3, 4, 5, 10)

**Files:** `web/src/features/devices/LogExplorer.tsx` (+ `.test.tsx`), `web/src/components/icons.tsx` (add `ChevronLeftIcon`/`ChevronRightIcon`), `web/src/features/devices/state/device-store.ts` (mount fetch for units), `web/src/features/devices/SystemLogs.tsx` / `DeviceLogs.tsx` if a mount-load prop is needed.

**3 — Resizable + collapsible:**
- Replace the fixed `max-h-96` entries container with a `resize-y overflow-auto` container (native CSS handle; `min-h`/`max-h` guards). No new deps.
- Add a collapse caret on the card header (same visual pattern as Intel AMT Setup — `▶` rotate-90) toggling the entries region; state is per-pane (agent/system independent).

**4 — Prev/next arrows:** the store already *replaces* entries per offset fetch (`device-store.ts:240`), so this is offset paging, not append. Replace the single "Load More" with `‹`/`›` icon buttons: `‹` = `runFetch(offset − LIMIT)` disabled at `offset === 0`; `›` = `runFetch(offset + LIMIT)` disabled when `!has_more`. Keep the "Showing X-Y of N" readout. `aria-label`s: "Previous page" / "Next page".

**5 — Recolor:** "Load More"→arrows and "View logs for this window" use the Restart-Agent palette `bg-yellow-600 hover:bg-yellow-700` (`DeviceDetail.tsx:322`).

**10 — Units from start:** on System Logs mount, auto-fetch the most-recent default window (e.g. last 1 h) once so `available_units` + recent logs populate immediately. Guard against double-fetch when a `focusWindow` is also supplied (focus wins).

**Edge cases:** paging past the last full page (partial page) keeps `‹` enabled; changing level/unit/search/window resets `offset` to 0 (already the case); resize persists only for the session (no stored pref this pass); collapse hides the entries + pager but keeps the filter bar.

**TDD:** pager boundary tests (prev disabled at 0, next disabled at `!has_more`, offset math) FIRST; collapse toggles entries visibility; mount triggers exactly one units/logs fetch. **Determinism:** no `.only`/`.skip` (test-skip-guard).

**DoD:** System Logs shows units on open; the entries area is drag-resizable and collapsible; navigation is via ‹/› arrows; the three buttons are yellow.

---

## Workstream C — Telemetry UI (items 6, 7, 8)

**Files:** `web/src/features/devices/DeviceMetrics.tsx` (+ `.test.tsx`), `web/src/features/devices/charts/TimeSeriesChart.tsx` (+ `.test.tsx`), `web/src/features/devices/DeviceDetail.tsx`.

**6 — Start Session green:** `DeviceDetail.tsx:312` `bg-blue-600 hover:bg-blue-700` → `bg-green-500 hover:bg-green-600` (online-dot green per `StatusBadge.tsx:8`).

**7 — Drag freeze:** add `selectionActive = selectedWindow != null`. The 30 s `setInterval(load)` (`DeviceMetrics.tsx:158-161`) is gated: when a selection is active, skip the reload so `setData` never wipes the uPlot select overlay and the preset never re-applies. The existing ✕/Clear (or a new "Clear selection" affordance) sets `selectedWindow=null` → polling resumes and `load()` fires once. The full preset window stays visible with the drag highlight + correlation table throughout.
- Implementation detail: uPlot's select overlay is cleared by `setData`; freezing the poll is sufficient. Also persist the visual `select` rect across the *manual* preset-unchanged renders (verify the overlay survives a no-op commit).

**8 — Text cleanup:**
- Remove `bandCaption` + its `<span>` (`DeviceMetrics.tsx:29-34, 243`). Keep the band series/fill in `aligned-data.ts` untouched. (Optional: provenance as a `title=` tooltip on the family header.)
- Remove the "Time --" legend row: add `legend: { show: false }` to the uPlot opts in `TimeSeriesChart.tsx:67-75`.

**14 — Chart clips the data line at top/bottom:** the y-scale range is computed correctly per poll in `aligned-data.ts:112-116` (`[lo-pad, hi+pad]`, 5 % pad), but `TimeSeriesChart` applies `yRange` **only in the mount effect** (keyed on `structureKey`); the `setData` update effect (`TimeSeriesChart.tsx:89-91`) never re-applies the scale. So when the 30 s poll brings a new peak/trough beyond the *initial* window's range, the stale fixed range clips the line until the series structure changes.
- Fix: add a `useLayoutEffect` keyed on `yRange` that calls `chartRef.current?.setScale('y', { min: yr[0], max: yr[1] })` (and clears back to auto when `yRange` is null). Keeps the deliberate band-inclusive range while eliminating the clip.
- Also account for stroke half-width at the extremes: the `width:1.5` stroke centered on a boundary value shaves ~0.75 px; the 5 % pad usually covers it, but confirm on flat/near-constant series (e.g. disk pinned at 55.5 %) where `hi≈lo`.

**Edge cases:** clearing the window while a correlate request is in-flight cancels/ignores the stale result (debounce ref already exists); disabling the legend must not break the hover cursor; freeze must still allow a manual preset change (changing preset clears the selection first); re-applying the y-scale must not fight uPlot's x-drag zoom (x and y scales are independent; y `setScale` leaves x untouched).

**TDD:** chart adapter test asserts `legend.show === false` in constructed opts and that a drag still fires `onSelectWindow`; a test drives `setData` with data whose extrema exceed the initial `yRange` and asserts `setScale('y', …)` is called with the new bounds (clip regression); `DeviceMetrics` test asserts the poll is suppressed while a selection is active and resumes on clear (fake timers); band caption no longer rendered.

**DoD:** dragging a window keeps the highlight + correlation indefinitely (no 30 s reset); Clear resumes live updates; no band caption, no "Time --"; the data line is never clipped at the top/bottom after a poll brings new extrema.

---

## Workstream D — Network metric → primary-interface rate (item 9)

**Highest-risk workstream.** cpu/mem/disk are already correct (verified) — no change. Only `net` changes: cumulative-bytes-over-all-interfaces → **per-second rate on the primary (default-route) interface**.

**Files:**
- `agent/crates/mesh-agent-core/src/ml/sampler.rs` — `MetricSample.network_*` become rates; `SysinfoSampler` holds previous `(bytes, ts)` for the chosen iface + a `primary_interface()` resolver; compute `Δbytes/Δt`.
- new `agent/crates/mesh-agent-core/src/ml/primary_iface.rs` (+ tests) — cross-platform default-route interface resolver.
- `agent/crates/mesh-agent-core/src/ml/store_sink.rs` — rename series labels `net.rx_bytes`/`net.tx_bytes` → `net.rx_bps`/`net.tx_bps` in `series_dim_name`/`dim_series`; reconsider series scaling (fractional rate).
- `agent/crates/mesh-agent-core/src/ml/host_metric_stream.rs` — `sample_values` feeds the rate; docstrings updated to say "rate" (live == backfill invariant preserved).
- Golden: repoint the `AgentMetricWindow` golden host dims (per memory note); `server/internal/protocol` golden if dim names are asserted there.
- `web/src/features/devices/charts/aligned-data.ts` — `familyCurrentLabel`/`isPercentDimension` + format net as `1.4 MB/s` (rate) not cumulative bytes.

**Design rationale (proof-backed):** the server stores every dim as a plain averaged gauge (`opengate_edge_metric_avg`) — no `rate()`/`increase()` (`vm_query.go`, `metrics_assemble.go`). Averaging a *cumulative counter* over a 10 s window (current behavior) is meaningless; a **rate gauge** averages correctly through the identical `roll_to_10s` path (`backfill.rs:383`), so live and reconnect-backfill stay byte-identical with **no server change**.

**Primary-interface resolver (cross-platform, auto-detect, silent no-op on failure):**
- Linux: parse `/proc/net/route` for destination `00000000` (default route) → iface name.
- Windows: best-route API (`GetBestInterface`/forward table) or documented fallback.
- macOS: `route -n get default` / sysctl.
- Fallback order if unresolved: highest-traffic non-loopback iface → else emit `null` for the net dims (never a wrong number). No new manual install; degrade silently where unsupported (matches the zero-manual-install rule).

**Rate/counter edge cases:** first sample (no prev) → null; iface changed between samples → reset prev, null this tick; counter reset/wrap (`now < prev`) → null; `Δt <= 0` → null. Nulls render as line breaks (already handled by `toFloat32`/`spanGaps:false`).

**TDD:** resolver tests over fixture `/proc/net/route` content in a `TempDir` (deterministic — no reading the host's real routing table); rate math tests (normal, first-sample, wrap, iface-change, zero-dt); windower/backfill equality test updated for the renamed dims; web format test for `MB/s`.

**DoD:** the net family charts show throughput (B/s) for the primary NIC that idles toward ~0, not an ever-climbing counter; golden tests green; backfill == live preserved.

---

## Workstream E — Edge-health "No data" (item 11) — LIVE-VERIFIED ROOT CAUSE

**Investigation complete (2026-07-24, live OKE cluster + agent journal). The original "widen the badge query" fix is REFUTED.**

### Empirical findings (proofs)

- **Symptom reproduced:** instant query `opengate_edge_node_anomaly_rate` at `now` → **empty** (badge "No data"), while `opengate_edge_metric_avg` (charts) resolves. One active agent: `f6ee3df7-…` (this WSL host).
- **The series is starved, not stale:** only **3** anomaly samples in 24 h (deltas 420 s, 540 s), all during a *stable, trained* window — so `last_over_time([10m])` returns empty (last sample 12.5 min old). No lookback rescues a series this sparse.
- **The loop is healthy:** host `cpu.total` runs a clean 10 s cadence (42×10 s, few 30 s gaps) over the same 15 min the anomaly series is silent → not the sampler, not the connection (stable ~80 min), not training (`ensemble trained`×1, **0** re-baseline / **0** train-fail / **0** maintenance / **0** channel-full).
- **Server counters pin the loss (24 h):** `edge_telemetry_ingested_total{type="AgentHealthSummary"}` = **1274 accepted**, yet only **3 persisted** to VM; `edge_telemetry_drops_total{reason="persist_slots_full"}` = **1524** (dominant), `interval_floor` = 672, `persist_failed` = 3. → **~1271 accepted anomaly summaries dropped at the persist stage.**

### Root cause

`persistTelemetry` (`server/internal/agentapi/conn_telemetry.go:185-207`) is a **best-effort, 4-slot** path (`telemetryConcurrentWrites = 4`): when all slots are busy it **drops** (`persist_slots_full`) instead of queuing. Each 60 s heartbeat delivers a batch with **host-metric windows first, the single anomaly summary last** (`agent/.../main.rs:727-730`). The dense host-window firehose (≈6/heartbeat, each write held up to `telemetryPersistTimeout` = 2 s) occupies all 4 slots, so the tail-ordered `AgentHealthSummary` loses the slot race ~99.8 % of the time. The badge query is fine; **the sample is dropped before it is ever written**.

Secondary: `interval_floor` (672) drops back-to-back same-type messages <10 s apart (host windows batched on reconnect). Early connection churn (3 reconnects in the first 9 min, then stable) is a real but separate concern tracked by the existing fast-path-reconnect work — not the badge root cause.

### Fix — LOCKED: E1 (coalesce) + E4 (lookback safety net)

Stop the low-rate, high-value `AgentHealthSummary` from losing the persist-slot race to the host-metric firehose, at the true cause.

**E1 — coalesce telemetry persistence per connection (primary):**
- Buffer `[]telemetry.Sample` on the `AgentConn` as each telemetry handler (`handleAgentMetricWindow`, `handleAgentHealthSummary`, `handleHealthWindowResponse`, and the numeric samples of `handleProcessReport`) produces samples, instead of each calling `persistTelemetry` independently. `handleControl` (`conn.go:356`) is a single per-connection read loop, so the buffer needs no extra locking.
- Flush the buffer as **one** `WriteSamples(ctx, org, device, samples)` via **one** persist slot, on a short debounce/size trigger (the heartbeat delivers the whole batch in a tight burst, so a small debounce coalesces it into one write). `handleProcessReport`'s `UpsertReport` (separate RLS table) keeps its own write; only its numeric samples join the buffer.
- **Flush on disconnect** (control loop exit / conn teardown) so buffered samples are never lost.
- Keep the 4-slot pool as-is; with coalescing, per-connection write concurrency drops to ~1 flush/interval, so the slots stop saturating and the tail-drop disappears.

**E4 — reader-side safety net (defense-in-depth):** `enrichAnomalyRates` (`handlers_device_metrics.go:40`) uses `last_over_time(opengate_edge_node_anomaly_rate[10m])` at `now` instead of the bare selector, so a brief gap never blanks the badge. Add a bounded-lookback variant to the telemetry reader (`vm_query.go`), tenant-scoped via `scopedSelector`. (With E1 the series is ~1/min and even the bare query works; E4 is one line of insurance.)

**Observability:** surface `persist_slots_full` / `interval_floor` beyond `debug` (they masked a 99.8 % loss). The `edge_telemetry_drops_total` counter already exists — add a drop-ratio alert rule so a regression is caught, not silently absorbed.

Alternatives **E2** (reserve a slot — minimal-diff fallback) and **E3** (bounded queue + backpressure — deferred: its backpressure semantics deserve a dedicated review, not this combined PR) are recorded below.

**Files:** `server/internal/agentapi/conn_telemetry.go` + `conn.go` (buffer field) (+ `_test.go`); `server/internal/api/handlers_device_metrics.go` + `server/internal/telemetry/vm_query.go` (+ tests) for E4; alert rule under `deploy/` for the drop-ratio signal.

**TDD:**
- E1: drive a heartbeat-shaped batch (N host windows + 1 tail health summary) through the persist path under a saturated/slow writer and assert the health summary is **persisted, not dropped** (regression proof for the tail-drop); assert the batch coalesces into **one** `WriteSamples` call.
- E1 edge: a mid-stream disconnect **flushes** the buffer (no silent loss of buffered samples).
- E4: `testvm` write of a rate sample stamped ~5 min old → `enrichAnomalyRates` returns it with `last_over_time([10m])` but **not** with the bare instant selector (proves the safety net).
- `testvm`-backed; never skips.

**DoD:** `edge_telemetry_ingested_total{type="AgentHealthSummary"}` ≈ persisted anomaly samples (drop ratio → ~0); the badge shows a live rate on a steady online agent; drop counters surfaced + alert rule in place. Re-verify live post-deploy: `AgentHealthSummary` accepted vs VM sample count converge, and host-window density rises toward the 10 s cadence.

### Scope note

This is no longer a one-line change — it is a server-side telemetry-persistence fix touching the hot ingest path. **Decision: stays in the combined PR** (user choice). Because it touches the hot ingest path, it carries its own load/soak validation and drop-counter re-verification within the combined PR's DoD; keep the diff isolated to `conn_telemetry.go` so review can treat it independently from the UI changes.

### Fix options — detail (primary approach TBD by user)

`handleControl` (`conn.go:356`) processes control messages **sequentially** in one per-connection read loop; `WriteSamples(ctx, org, device, []Sample)` already takes a batch. Current path: **each** telemetry message → its own `persistTelemetry` → try-acquire 1 of 4 slots → 2 s-timeout goroutine → **drop if full**.

- **E1 — Coalesce (micro-batch) into one write per interval.** Buffer `[]telemetry.Sample` on the conn as messages are handled; flush as a single `WriteSamples` on a short debounce/size trigger (the read loop is single-threaded per conn, so no extra locking; process reports keep their own `UpsertReport`). *Pros:* removes the burst that exhausts the slots → fixes the anomaly starvation **and** host-window thinning (the "chart gaps"); fewer, larger VM writes (VM prefers this) → better at scale; no per-type special-casing. *Cons:* adds a buffer + flush timing on the hot path; small persistence latency; must flush on disconnect so buffered samples aren't lost. *Risk: moderate.*
- **E2 — Reserve a slot/pool for `AgentHealthSummary`.** Dedicated small persist capacity for the anomaly summary so it never contends with host windows. *Pros:* smallest, most targeted, low-risk; directly fixes the badge. *Cons:* band-aid — host-window thinning remains; per-type special-casing doesn't scale as telemetry types grow. *Risk: low.*
- **E3 — Bounded queue + backpressure (no silent drop).** Replace best-effort try-or-drop with a bounded FIFO drained by a worker pool; full → brief backpressure, not drop. *Pros:* most general/robust; FIFO fairness removes the tail-ordering bias; observable. *Cons:* backpressure can delay other control messages if VM is slow — must bound carefully; largest hot-path behavioral change; needs the most load validation. *Risk: moderate-high.*
- **E4 — Reader-side `last_over_time([10m])` (defense-in-depth only).** Layer on E1/E2/E3 as cheap insurance for brief gaps; useless alone (refuted).

**LOCKED: E1 + E4.** E1 fixes both symptoms the user reported (badge blank *and* chart gaps) at the true cause and improves write efficiency for scale; E4 is a one-line safety net. E2 (reserve-a-slot) is the recorded minimal-diff fallback; E3 (bounded queue) is deferred to a dedicated review.

---

## Workstream F — Trivial fixes (items 12, 13)

**12 — Group ID N/A:** `DeviceDetail.tsx:370` `group_id?.trim()` passes the all-zeros UUID. Add `isUnassignedGroup(id)` helper (empty OR `00000000-0000-0000-0000-000000000000`) → render "N/A"; reuse it in the Move-to-Group guard. (+ unit test.)

**13 — Hardware collapsible:** wrap the Hardware section (`DeviceDetail.tsx:435-483`) in the same caret-toggle pattern as Intel AMT Setup (`showHardware` state, `▶` rotate-90 header button). The Refresh-Hardware button stays in the header row. (+ test asserting toggle hides/shows the `<dl>`.)

---

## Cross-cutting requirements

- **TDD mandate:** every source edit is preceded by a failing test on this branch (hook-enforced). Positive + negative cases.
- **Tests always run:** no `t.Skip`, no `.only/.skip/.todo`, no `#[ignore]` (test-skip-guard).
- **Determinism:** resolver + VM tests use `TempDir`/`testvm` fixtures, never host state.
- **Docs live-state:** rename net dims and update docstrings to describe the *rate* (no "used to be cumulative" narration). Update `/docs` telemetry page for the net-metric meaning and the badge lookback.
- **SonarCloud:** watch for duplicated literals in new web/Go files; no suppressions.
- **Completion:** update `.claude/phases.md` (Completed row), add a `techdebt.md` note that client-side status filtering is a future server-side concern at >20k agents, and **archive this plan in the same commit** (git mv → `plans/archive/`, bump internal links one `../` deeper, repoint the phases row).

## Risk register

| Risk | Mitigation |
|---|---|
| Primary-interface detection is platform-specific and has no prior art here | Fixture-driven tests per platform; documented fallback chain ending in `null` (never a wrong number); silent no-op where unsupported |
| Local redb TSDB scaling for a fractional B/s rate (percent gauges use ×100 fixed-point; bytes rode the integer path) | Decide series scale explicitly in `store_sink.open`; test round-trip precision |
| Golden `AgentMetricWindow` drift on dim rename | Update golden in the same commit; golden test is the gate |
| Drag-freeze could stall live data if "clear" is missed | Always-visible Clear affordance; preset change also clears |
| Auto-load logs on mount adds an agent round-trip per device open | Scope to one default window; `focusWindow` short-circuits |

## Reviewer checklist

- [ ] Dashboard + Fleet-Health cards deep-link to correct filtered `/devices?…`; chip clears.
- [ ] Logs entries resizable + collapsible; ‹/› paging bounds correct; three buttons yellow; units present on open.
- [ ] Start Session green; drag highlight + correlation survive ≥60 s; no band caption / no "Time --"; line not clipped after a poll brings new extrema.
- [ ] net family shows primary-iface B/s (idles to ~0); golden green; backfill==live.
- [ ] Persist-fairness test proves a tail-of-batch `AgentHealthSummary` is persisted, not dropped; post-deploy live re-check shows accepted≈persisted for anomaly summaries; drop counters surfaced.
- [ ] Group ID "N/A" for zero UUID; Hardware collapses like AMT.
- [ ] `/precommit` gauntlet green; `phases.md` + `techdebt.md` updated; this plan archived in the landing commit.
