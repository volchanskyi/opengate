# WS-15 — Reconnect backfill (tiered, scheduled) + on-demand local-history pull

**Objective:** On reconnect, catch the central store up to the agent's persisted history (WS-14b)
as a **server-coordinated, prioritized, resolution-tiered, gradually-drained** flow — never a
stampede — and support **server-mediated on-demand pulls** of deep/full-res per-host history from
the agent's local tiers. Because local data is durable, backfill has **no urgency and never loses
data**; the only questions are order, resolution, and protecting the single node.

**Dependencies:** WS-14b (tiered store + cursor; gated by the WS-14a spike), WS-3 (wire + caps),
WS-4 (VM tiers + scoped reader), WS-1 (capability). **Wave:** after WS-14b.

## Locked decisions

- **Resolution-tiered backfill (local tier → matching VM tier; 1 s never sent):**
  | Gap age | Local source | Sent as | Lands in |
  |---|---|---|---|
  | last 24–48 h | T0 (1 s) | 10 s windows | VM raw tier |
  | older, up to retention | T1 (1 min) | 1 min points | VM 1 min rollup |
  | very old | T2 (1 hr) | 1 hr points | VM 1 hr rollup |
  | full-res 1 s, any age | stays local | — | **on-demand pull only** |
  Collapses a 200-agent/30-day storm from ~51.8 B samples (naïve 1 s) to ~1.15 B (~45× less).
- **Hybrid ordering:** live always current; backfill the **recent window first** (10 s), then drain
  the older 1 min/1 hr remainder oldest-first from a watermark. Robust to interruption.
- **Clamp to configured VM retention** (≈90 d, parameterized — WS-4/WS-15b; not a hard cap): never
  push samples older than `now − retention`; older history reachable via on-demand pull.
- **Backfill MUST bypass live stream-aggregation (correctness-critical).** VM stream-aggregation by
  default **ignores input timestamps and aggregates by ingestion time**, `ignoreOldSamples` *drops*
  old samples, and its state is in-memory — so routing historical rollups through `-streamAggr`
  produces **mathematically wrong charts** (bucketed by arrival, not real time). Backfill therefore
  writes **pre-rolled samples directly to the explicit rollup series with their original
  timestamps** via the VM **import API** (`/api/v1/import`), never through the live `-streamAggr`
  pipeline. Live 10 s telemetry may use stream-agg; backfill may not.
  (Proof: https://docs.victoriametrics.com/stream-aggregation/.)
- **New device on reinstall:** a wiped host returns as a fresh identity (no pre-wipe backfill); the
  old device series ages out at retention. Reconnect **re-validates cert/enrollment + triggers the
  update check**; long-stale old devices age into archivable state (device-lifecycle).
- **Server-coordinated backfill scheduler** (below).

## Backfill scheduler

- **Priority classes (strict preemption):** P0 control-plane > P1 live telemetry > P2 recent-window
  backfill > P3 historical-rollup backfill. Backfill always yields to live + control.
- **Admission:** global concurrency cap + a **load-adaptive token-bucket ingest budget** computed
  from live headroom (server CPU, live ingest rate, VM ingest/query latency, the WS-15b control-plane
  p99 budget). Agents **request a slot** → `GrantBackfill{rate, deadline}` or
  `DeferBackfill{retry_after}`; deferred agents hold durable data and retry with jittered backoff.
- **Fairness:** weighted **max-min fair-share across orgs** (per-tenant concurrency cap); within a
  tenant FIFO with **aging** so none starve; P2 granted ahead of P3 across tenants.
- **Scale-out (future):** single replica today → in-memory scheduler; if KEDA scales >1 replica
  (ADR-034), gate on a shared **VM-ingest-rate** signal rather than per-replica counters.

## File inventory

- **Reuse (do not rebuild):** [`server/internal/testvm`](../../server/internal/testvm/testvm.go)
  (`testvm.BaseURL(t)`) for the VM bucket-correctness test (Step 6);
  [`server/tests/vmbackfill`](../../server/tests/vmbackfill/spike_test.go) already proves the import
  API preserves original timestamps (import-not-stream-agg) — reuse its import + `/api/v1/export`
  assertion pattern.
- **Modify:** [`control.rs`](../../agent/crates/mesh-protocol/src/control.rs) / Go protocol —
  additive, capability-gated: `MetricBackfillBatch { tier, historical samples, cursor }`,
  `RequestLocalHistory`/`LocalHistoryResponse` (bounded), `RequestBackfillSlot`/`GrantBackfill`/
  `DeferBackfill`; goldens.
- **Modify:** [`main.rs`](../../agent/crates/mesh-agent/src/main.rs) — request a slot; replay
  **recent-first then older**, **tier-mapped**, throttled to the granted rate, resumable from the
  WS-14b cursor (advance only on per-batch ack); answer `RequestLocalHistory` from local tiers;
  **timestamp sanity bounds** (reject/skew-correct wild clocks).
- **Modify:** server [`conn.go`](../../server/internal/agentapi/conn.go) — the **scheduler**
  (admission/budget/fair-share); accept backfill batches → WS-4 VM client at the **matching tier**,
  **clamped to retention**, deduped (VM by timestamp); broker `RequestLocalHistory` (bounded);
  scope everything to the connection's device→org.
- **Modify:** [`api/openapi.yaml`](../../api/openapi.yaml) + [`api.go`](../../server/internal/api/api.go)
  — deep-history endpoint brokering `RequestLocalHistory`; regen Go + TS.

## Steps (TDD-first)

1. **Test first:** offline→online replay — recent-window 10 s first then older 1 min/1 hr; **in
   order within tier, resumable from cursor, de-duped**, **clamped to retention** → implement.
2. **Test first:** scheduler — global + per-tenant caps; load-adaptive budget shrinks under live
   load; grant/defer + jittered backoff; **fair-share across orgs with aging** (no starvation) →
   implement.
3. **Test first:** clock skew — wild/old/future timestamps are bounded/corrected; out-of-retention
   skipped → implement.
4. **Test first (cross-lang):** all new messages round-trip + capability-gated; `make golden`.
5. **Test first (server):** backfill lands in the right VM tier (scoped `org_id`); deep-history
   broker bounded + cross-tenant deny; reconnect re-validates identity → implement; regen OpenAPI.
6. **Test first (VM bucket correctness):** a real-VM integration test imports historical rollups
   with **old timestamps** after a simulated reconnect delay, then `query_range`-verifies the
   samples land in the **correct time buckets** (proving backfill bypassed stream-agg). Throwaway VM
   via `server/internal/testvm`; reuse the `server/tests/vmbackfill` import + `/api/v1/export` pattern.

## Gotchas / constraints

- **No urgency, no loss:** durable local data drains gradually; live + control always preempt
  backfill; the scheduler can be stingy.
- Replay **idempotent + resumable**; cursor skips evicted ranges (WS-14b) without stalling.
- 1 s never sent to VM; full-res old data is on-demand local only; deep-history pull is single-host,
  server-brokered, bounded (no browser→agent, no fan-out).
- Authz scope from the connection, never agent-supplied org.

## Reviewer checklist

- [ ] Tiered backfill (local→VM tier; 1 s never sent); recent-first hybrid order; clamped to retention.
- [ ] Scheduler: global + per-tenant caps, load-adaptive budget, grant/defer, fair-share + aging.
- [ ] Clock bounds; idempotent/resumable; cursor skips evicted ranges.
- [ ] New messages additive + capability-gated; goldens green; backfill → correct VM tier (scoped).
- [ ] Deep-history broker bounded + cross-tenant deny; reconnect re-validates identity; OpenAPI regen.
- [ ] `/precommit` green.

## Verification

`make golden`; `cd agent && cargo test -p mesh-agent-core`; `cd server && go test ./internal/agentapi/... ./internal/api/...`.
Manual: kill connectivity for a long window under load, restore → recent detail returns first then
history drains without starving live/control; a fleet-wide reconnect drains gradually; deep-history
pull returns older-than-VM data. `/precommit` green. `/docs`: Monitoring + Multiscale-Readiness.
