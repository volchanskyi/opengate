# Edge-Sentinel — Netdata-informed local anomaly detection + multi-tenant RMM telemetry

> **Status:** Finalized planning — **evidence-first, gated rollout**. A **Wave 0 feasibility
> gate** must pass before feature work; measurement is a **build gate**, not a trailing step.
> Runtime behavior ships behind a default-off flag and degrades silently (the sentinel must
> never block remote management). Supersedes the deleted drafts and the prior Timescale-based
> revision (a third-party analyst review + live OCI/cluster data drove the pivot below).
>
> This is the **master plan + micro-plan index**. Active per-workstream execution specs are
> sibling `edge-sentinel-ws-N-*.md` files in this directory; completed specs are archived under
> `archive/`. Per the doc-link rule,
> plans reference **other plans** by plain path/code span (never markdown links) — only
> repo source/docs are linked.

## Context

Evolve the reactive Rust agent into an **edge sentinel** — profile host health locally,
detect anomalies with no labeled data (Netdata's unsupervised, zero-config engine), and
surface insight to the server — framed for OpenGate's domain: a **multi-tenant RMM**.

A third-party architect reviewed the prior plan; this session verified every claim against the
code **and live OCI/cluster data**. The confirmed findings:

1. **Protocol compat was overstated, in *both* directions.** Agent→server: unknown control type
   → `ErrUnexpectedMessage` ([conn.go:245](../../server/internal/agentapi/conn.go#L245)) → the
   control loop drops the connection ([server.go:260](../../server/internal/agentapi/server.go#L260)),
   pinned by [conn_part8_test.go:44](../../server/internal/agentapi/conn_part8_test.go#L44). Server→agent:
   the Rust enum is `#[serde(tag="type")]`+`#[non_exhaustive]` — `#[non_exhaustive]` does **not**
   make serde tolerate unknown tags, so a new server→agent variant breaks old agents at decode.
   WS-1 now fixes **both** directions + adds capability negotiation.
2. **Tenancy is a product migration, not a label.** Confirmed **zero** `org_id`/tenant/RLS
   anywhere ([001_initial.up.sql](../../server/internal/db/migrations/001_initial.up.sql)).
   Today's authz is `users.is_admin` + `security_groups` RBAC. WS-0 is a standalone migration.
3. **The Timescale hot-store design was fatal as written, and is replaced.** Upstream TimescaleDB
   issues #6827 + #5787 (both open) prove **RLS cannot coexist with compression or continuous
   aggregates** on a hypertable — the original WS-4 required all three. Live `df` then showed the
   premise was also wrong: every block volume is **<1% used** (VictoriaMetrics `/storage` = 26 MB
   of 48.9 GB; Postgres = 63 MB). The fix: put numeric telemetry in the **already-deployed
   VictoriaMetrics** (off the control-plane Postgres, purpose-built compression, Apache-2,
   Grafana wired), with OSS **stream aggregation** for downsampling; keep sensitive process PII
   in a Postgres **RLS** table.

### Decisions locked (this session)

| Topic | Decision |
|---|---|
| Edge intelligence | Clean-room **k=2 k-means ensemble** (Netdata shape: ~18 staggered models/metric, consensus, **99th-pct** threshold), ARM-measured **in Wave 0** before default-on |
| License | **Clean-room only** — no vendored/ported Netdata code (GPL-3 vs workspace Apache-2, [agent/Cargo.toml](../../agent/Cargo.toml)) |
| **Tenancy** | **Full product-wide multi-tenancy + Postgres RLS now** (WS-0 foundation, standalone migration with rehearsal/rollback) |
| **Hot store (numeric)** | **VictoriaMetrics (OSS)** — server `remote_write`, `org_id` label, app-layer scoped reads; **off the control-plane Postgres** |
| **Downsampling** | VM OSS **stream aggregation** (1-min/1-hr rollups) + short raw retention — *not* VM Enterprise downsampling |
| **Aggregates** | **Central = `avg` only** (VM); **min/max/last + 1 s raw stay agent-local** (WS-14b), on-demand via WS-15. Aggregates are separate series, so central-all-four would ~4× cardinality. Per-entity expansion (cores/disks/ifaces) **capped** in the schema budget |
| **Backfill vs stream-agg** | Live 10 s may use stream-agg; **historical backfill writes pre-rolled rollups directly via the import API with original timestamps** (stream-agg buckets by arrival → wrong) |
| **Metric families** | CPU + memory + disk + network + **process/service** (numeric top-N by rank) |
| **Process PII** | Default = **basename + rank + pid + cpu/mem + optional cmdline hash** in the RLS table; **full cmdline off by default**, on-demand only (audited, time-limited, elevated permission); server redaction = defense-in-depth |
| Correlation | **On-demand**, server-side **KS-test in Go over VM (MetricsQL) pulls** via the scoped query client; bounded concurrency/timeout |
| Cold tier | **VM retention + stream-agg rollups** primary; **optional** server-side Parquet archival via write-only PAR; **DuckDB deferred** (no CGO dep this rollout) |
| Signal action | **Investigation-aid only first** (no auto-notify until FPR soak) |
| Visibility | **Any device-viewer in the org** |
| Protocol | **Bidirectional** forward-compat + **capability negotiation**; flip the pinning test |
| Web chart engine | **Thin adapter over uPlot** (canvas-2D); React owns chrome, the renderer owns pixels via typed arrays (see `archive/edge-sentinel-ws-6-web-ui.md`) |

### Headline risk (largely eliminated; what remains is gated)

The original headline risk — telemetry sharing the Postgres that runs live relay/WebRTC/device
control — is **removed**: numeric telemetry now lives in **VictoriaMetrics**, a separate process
on a separate (near-empty) volume. The control-plane Postgres only gains a small RLS process
table. The residual risks are (a) **app-layer numeric tenancy** (VM has no RLS — mitigated by a
single scoped query client + per-endpoint cross-tenant-deny tests + a CI grep gate; sensitive
PII stays fail-closed in Postgres RLS), and (b) **VM resource growth** under load (proven in
Wave 0 against the existing ~48.9 GB headroom). Both are **Wave 0 gate items**, not accepted
unknowns.

## Architecture

```
 Managed host                                       OKE Always-Free (≈2 OCPU/12 GB, 1 node)
┌──────────────────────────────────────┐          ┌───────────────────────────────────────┐
│ sampler(cpu/mem/disk/net/proc) → ring │  mTLS    │ control handler (cap-gated, fwd-compat)│
│   bounded RAM, 1s (Tier 0, ephemeral) │  QUIC    │  ├ health/metric summary → server      │
│ detector: k=2 ▸ consensus ▸ 99th-pct  ├─────────▶│  │   └→ remote_write → VictoriaMetrics │
│   per-family + node anomaly rate      │ summary  │  │        └ stream-agg 1m/1h rollups    │
│ + org_id; proc basename(+hash) only   │ + pull   │  ├ process basename+hash → Postgres RLS│
│ (full cmdline on-demand/audited)      │          │  └ correlation: Go KS-test ◀ MetricsQL │
└───────────────────────────────┬───────┘          │ web: badge + panel + uPlot + drilldown │
                                 ▼ (optional, batched)└───────────────────────────────────────┘
            OCI Object Storage (20 GB free, archival Parquet, tenant-partitioned)
```

Tiering: **T0** = agent local store (1 s) — RAM ring initially, made a **persistent multi-tier
on-disk TSDB by WS-14b** (substrate chosen by the WS-14a spike; T0/T1/T2, inline anomaly scores,
holds min/max/last + 1 s, offline-resilient); **hot** =
VictoriaMetrics (10 s raw, short retention); **rollup** = VM stream-agg 1 min/1 hr (long retention);
**archival (optional)** = Parquet/object. Agents are outbound-only (NAT); the **server is the only
writer** to the central stores — no standing agent credentials. The agent local TSDB is the
full-res sovereign copy; deep per-host history is pulled server-mediated on-demand (WS-15).

## Workstreams (gated rollout, dependency-ordered)

Each micro-plan is self-contained (objective, file inventory, TDD steps, gotchas, reviewer
checklist, verification). Each: **TDD first**, then `make golden`/tests, `/precommit`, commit,
`/refactor`, push.

| WS | Micro-plan | Summary |
|---|---|---|
| WS-0 | `archive/edge-sentinel-ws-0-tenancy-rls.md` | product-wide `org_id` + Postgres RLS foundation (standalone migration) |
| WS-1 | `archive/edge-sentinel-ws-1-protocol-forward-compat.md` | bidirectional unknown-type tolerance + capability negotiation |
| WS-2 | `archive/edge-sentinel-ws-2-edge-ml-sampler.md` | clean-room k-means ensemble + `sysinfo` sampler (agent); ARM-benched |
| WS-3 | `archive/edge-sentinel-ws-3-wire-contract.md` | additive `ControlMessage` variants + Rust↔Go goldens; payload caps |
| WS-4 | `archive/edge-sentinel-ws-4-server-ingest-vm.md` | VictoriaMetrics ingest + stream-agg + Postgres process RLS table |
| WS-5 | `archive/edge-sentinel-ws-5-correlation-engine.md` | on-demand Go KS-test ranking over VM (MetricsQL) |
| WS-6 | `archive/edge-sentinel-ws-6-web-ui.md` | uPlot chart engine: badge + anomaly panel + timelines + drill-down |
| WS-7 | `archive/edge-sentinel-ws-7-cold-tier-duckdb.md` | VM retention/rollups + optional Parquet archival (DuckDB deferred) |
| WS-9 | `archive/edge-sentinel-ws-9-log-readers.md` | endpoint log readers (journald/syslog, Windows Event Log, self-logs) + rate extractor (agent) |
| WS-10 | `archive/edge-sentinel-ws-10-log-wire.md` | log-rate dims + extended on-demand query; capability-gated, golden-tested |
| WS-11 | `archive/edge-sentinel-ws-11-log-server.md` | rate dims → VM; on-demand raw broker + audit + elevated-permission gate |
| WS-12 | `archive/edge-sentinel-ws-12-log-web.md` | logs explorer + log-rate sparkline + metrics↔logs correlation jump |
| WS-13 | `archive/edge-sentinel-ws-13-log-privacy-ops.md` | raw-log redaction corpus + reader-sourcing ADR + Linux/Windows benchmark + soak |
| WS-14a | `archive/edge-sentinel-ws-14a-offline-tsdb-spike.md` (done — [ADR-051](../../docs/adr/ADR-051-edge-sentinel-local-tsdb-substrate.md): **redb** chosen) | local-TSDB substrate bake-off (append-only / redb / fjall / tsink / no-persist) on a fixture corpus + gates |
| WS-14b | `archive/edge-sentinel-ws-14b-offline-tsdb-build.md` (done — [ADR-052](../../docs/adr/ADR-052-edge-sentinel-local-tsdb-build.md): `store::LocalTsdb`, ~1.87 logical B/sample) | graduate the `edge-tsdb` crate to the production **redb** store (compact fixed-point codec, big-block packing, inline anomaly bit; holds min/max/last + 1 s) |
| WS-15 | `archive/edge-sentinel-ws-15-offline-backfill.md` (done) | reconnect backfill to VM (throttled) + on-demand server-mediated local-history pull |
| WS-15b | `archive/edge-sentinel-ws-15b-ops-measurement.md` | Grafana + sustained soak + default-on gate (harness/metrics/dashboard landed; default-on flip pending a real soak) |
| WS-16 | `archive/edge-sentinel-ws-16-discovery-agent.md` (done) | auto-discovery collectors (ports/services/DBs/containers/packages) + `DiscoveryReport` wire |
| WS-17 | `archive/edge-sentinel-ws-17-inventory-server.md` (done) | inventory RLS store + API |
| WS-18 | `edge-sentinel-ws-18-inventory-web.md` | inventory web view (generative dashboards deferred) |
| WS-19 | `edge-sentinel-ws-19-threshold-alerts.md` | declarative edge threshold alert rules (alongside ML anomaly) |
| WS-20 | `edge-sentinel-ws-20-data-lifecycle-deletion.md` | cascading erasure on device/tenant delete (VM + Postgres + cold + agent purge) |

### Execution sequencing (phased, small testable blocks)

**Principles.** Build in **small blocks that each end green and committable** (TDD: failing test →
implement → `/precommit`). Prefer **vertical slices** (agent→wire→server→web for one capability)
over horizontal layers, so every block is verifiable and delivers visible value. **Minimize manual
verification:** every gate is automated — `cargo`+clippy, `make golden`, Go `testpg`
(auto-starts Postgres + a throwaway VM via testcontainers), Playwright `make e2e`, and the load
harness. **Platform/hardware checks become CI, not hand checks:** ARM benches run on an ARM CI
runner (recorded artifact); journald/Windows-Event-Log/discovery readers are tested against
**captured fixtures** in CI; offline/backfill/storm/erasure are automated via throwaway VM +
simulated disconnect + the load harness. **Residual manual checks are consolidated into a single
final pass** (Phase 8): one real ARM device, one real Windows endpoint, one `/run` web smoke.

| Phase | Blocks (each a green `/precommit`) | Automated gate | Manual |
|---|---|---|---|
| **0 — Feasibility gate** | VM ingest spike @100/500 **measuring real active-series + per-entity expansion** (decide avg-only vs more); **backfill bucket-correctness via import API (not stream-agg)**; bidirectional protocol fixtures; tenancy migration rehearsal (backfill/deny/restore/rollback); ARM ML bench harness; privacy policy + redaction corpus | throwaway VM + `testpg` + `cargo` + golden; CI artifacts | none |
| **1 — Foundation (parallel)** | **1.1** WS-1 protocol+capability (merge **first**). **1.2** WS-0 tenancy split: 1.2a migration+RLS+deny → 1.2b OrgID claim+middleware → 1.2c repo tx threading → 1.2d web org ctx. **1.3** WS-2 ML+sampler functional | `make golden`; `testpg`; `cargo` | none |
| **2 — Metric spine (thin slice first)** | **2.1** WS-3 *minimal* `AgentHealthSummary` only → **2.2** WS-4 *minimal* VM client + ingest + scoped reader + process-table skeleton (first end-to-end metric) → **2.3** enrich: `AgentMetricWindow` + `ProcessReport` + stream-agg + 90 d retention | golden; throwaway VM + `testpg` | none |
| **3 — First visible product** | **3.1** WS-6 badge+timelines+range endpoint+chunk budget — bands ship with the **`avg_of_10s` fallback** (decoupled from WS-14x; true host min/max is enriched in Phase 5 via WS-15) → **3.2** WS-5 correlation | vitest + `make e2e` + `npm run size`; `go test` | none |
| **4 — Feature tracks (priority-ordered: D → T → L)** | **D** discovery: WS-16→17→18 (RMM table-stakes, RLS-only, lowest risk) → **T** alerts: WS-19 (smallest, high operator value) → **L** logs: WS-9→10→11→12→13 (heaviest — **WS-11 first resolves the `device_logs` raw-persistence precondition**). Tracks are independent and *may* parallelize given capacity; default order is value-/risk-first | `cargo`/golden/`testpg`/`make e2e` (fixtures for readers) | none (CI fixtures) |
| **5 — Offline durability (full track in v1)** | **5.1** WS-14a spike (5-candidate bake-off: append-only / redb / fjall / tsink / no-persist + rubric/gates) → **5.2** WS-14b graduate `edge-tsdb` → production **redb** store (compact fixed-point codec, big-block packing, inline anomaly bit, compression+footprint asserts, crash-recovery) → **5.3** WS-15 backfill+scheduler (offline→online, tiered, **import-not-streamAggr** bucket test, fairness, clock bounds, fleet-storm; **source of true min/max for WS-6 bands**) | `cargo` (+ ARM/Windows bench CI); throwaway VM + simulated disconnect + load harness | none |
| **6 — Cold tier (optional/deferrable)** | WS-7 VM retention/rollups (+ optional Parquet export) | `go test` | none |
| **7 — Lifecycle erasure** | WS-20 tombstone → orchestrator+cascade → GC → reconnect deprovision | `testpg` + throwaway VM + `make e2e` | none |
| **8 — Final soak + default-on** | WS-15b: sustained multi-tenant soak incl. logs/offline/discovery/alerts + fleet-reconnect storm; flip default-on only if budgets pass | load harness + dashboards | **consolidated:** 1 ARM device, 1 Windows endpoint, 1 web `/run` smoke |

**Critical path:** Phase 0 → WS-1/WS-0/WS-2 → WS-3 → WS-4 → WS-6/WS-5 → (feature tracks ‖ offline) →
WS-20 → WS-15b soak. **Everything hangs off WS-0 (tenancy) + WS-4 (VM ingest)** — they are the real
unblockers; land them early and stable. WS-6's `avg_of_10s` band fallback means Phase 3 (visible
product) **does not block on** the Phase-5 offline track; true min/max is a Phase-5 enrichment.

**Parallelism:** Phase 1 (WS-1/WS-0/WS-2) fully parallel; after WS-4, the Phase-4 feature tracks and
Phase-5 offline run independently. Phase-4 priority order is **discovery → alerts → logs**
(value-/risk-first); they may overlap if capacity allows, but logs lands last because of its
raw-broker + `device_logs` privacy precondition. **Merge WS-1 first** so every later additive wire
change is tolerated by older builds in both directions — the single biggest friction-reducer.

### WS-0 — Multi-tenancy foundation + RLS (prerequisite)
- New `organizations` table; add `org_id UUID NOT NULL` to every tenant table; **backfill** to a
  seeded default org; RLS `ENABLE`/`FORCE` + policies `USING (org_id = current_setting('app.current_org')::uuid)`;
  `is_admin` cross-org via a **policy** on a second GUC, **never** a `BYPASSRLS` app role.
- Add `OrgID` to JWT `Claims` ([auth.go:21](../../server/internal/auth/auth.go#L21)); tenant-
  context middleware after auth ([api.go:277](../../server/internal/api/api.go#L277)); per-tx
  `SET LOCAL app.current_org` through the repository layer.
- Migration **rehearsal + rollback** (Wave 0). CI grep gate forbids unscoped tenant queries.
- *Tests:* cross-tenant-deny suite (every repo); GUC set/cleared in-tx; admin bypass. *ADR:* RLS model.

### WS-1 — Protocol forward-compatibility (bidirectional) + capability negotiation
- Agent→server `default:` → **log-and-continue** ([conn.go:245](../../server/internal/agentapi/conn.go#L245));
  flip `TestAgentConn_HandleUnknownMessage`. Rust: unknown server→agent tag **decodes-and-ignores**.
- **Capability handshake** at register (reuse `AgentCapability`): server gates new server→agent
  variants on advertised support. Bidirectional golden fixtures (old×new both ways).

### WS-2 — Edge ML + sampler (agent, clean-room, pure Rust)
- New `mesh-agent-core/src/ml/`: k=2 ensemble (consensus, 99th-pct), bit-packed anomaly window;
  `MetricSampler` trait reusing **`sysinfo`** ([main.rs:739](../../agent/crates/mesh-agent/src/main.rs#L739)).
- Process top-N **by rank**, **basename + optional cmdline hash** only (no full cmdline by
  default). **ARM bench is a Wave 0 item**; default-off.
- *Tests:* `cargo test -p mesh-agent-core` (consensus, percentile, ring rollover, redaction of the
  on-demand path).

### WS-3 — Wire contract (additive, tenant-tagged, golden-gated)
- Additive `ControlMessage` variants ([control.rs](../../agent/crates/mesh-protocol/src/control.rs),
  `#[non_exhaustive]`), reusing `FRAME_CONTROL` (**no new QUIC stream**):
  `AgentHealthSummary`, `AgentMetricWindow` ({min,max,avg,last}@10 s), `ProcessReport`
  (basename + optional hash; **no full cmdline**), `RequestHealthWindow`/`HealthWindowResponse`
  (server→agent, **capability-gated**). **64 KiB telemetry payload cap**; control traffic
  prioritized, telemetry dropped (counter) under pressure.
- **Rust→Go goldens** ([golden_test.go](../../server/internal/protocol/golden_test.go)).

### WS-4 — Server ingest → VictoriaMetrics + Postgres process table
- Server **`remote_write` client** → VM (numeric, `org_id` label) + VM `-streamAggr.config`
  (1 min/1 hr rollups) + retention split; `device_processes` **Postgres RLS** table
  (basename, rank, pid, cpu, mem, optional cmdline hash). **No Timescale.**
- Handlers (mirror `handleHardwareReport`) → store-appropriate writes scoped by connection
  device→org; server-side redaction guard. A **scoped VM query client** (single choke point;
  injects the `org_id` label matcher) for WS-5/WS-6.
- *Tests:* throwaway VM + `testpg`; RLS deny on the process table; rollups produced.

### WS-5 — Correlation engine (on-demand, server-side over VM)
- `internal/correlate` + REST endpoint ([api/openapi.yaml](../../api/openapi.yaml)): fetch window
  series via the scoped VM client, rank top-N in Go (KS-test + anomaly-rate volume); bounded
  concurrency + timeout + fetch-size cap. Runs on the **server**, not the DB.
- *Tests:* injected anomaly ranks #1; label-scope deny; timeout/concurrency bounds.

### WS-6 — Web UI (uPlot chart engine)
- Thin uPlot adapter (canvas-2D). Health **badge** on the virtualized grid
  ([DeviceList.tsx](../../web/src/features/devices/DeviceList.tsx)); device-detail anomaly panel +
  timelines + correlation drill-down. Range endpoint guarantees `points ≤ maxPoints` (VM rollup
  pick + decimation). Lazy chart chunk + **explicit `charts` chunk size budget**. Full spec in
  `archive/edge-sentinel-ws-6-web-ui.md`. **Min/max bands:** central VM is `avg`-only, so chart bands carry
  a `min_max_source` provenance — true host min/max via the on-demand WS-15 local-history pull
  (`local`), with graceful fallback to `avg_of_10s` (min/max of 10 s averages, honestly labelled)
  or `none` when WS-14b/WS-15 are unavailable. **WS-6 bands therefore soft-depend on the
  spike-gated WS-14b** — never render a band the source can't back.
- *Tests:* adapter `setData` (mocked canvas); `make e2e`; chunk-size budget (`npm run size`).

### WS-7 — Cold tier (VM retention + optional Parquet archival)
- VM retention + stream-agg rollups are the long-term tier (set in WS-4). **Optional** server-side
  batched Parquet export of aged data to OCI Object Storage via write-only PAR (≤ daily, within
  50k req/mo; tenant-prefix-scoped). **DuckDB deferred** — no CGO dep unless a later ADR justifies.
- *Tests:* retention/rollup behavior; PAR scope/expiry if the export is built.

### WS-15b — Ops + soak + default-on gate (deferred until WS-15)
- Grafana dashboard (existing **VM datasource**): anomaly-rate, ingest, VM cardinality + disk
  growth, control-plane p99, correlation latency, drop count. Extend
  [loadtest](../../server/tests/loadtest/main.go) to **500 multi-tenant agents**; sustained soak.
  **Default-on only if every quality metric passes** (metrics **and** logs). Alerts deferred.
  Deferred until the offline track (WS-15) lands — the soak covers offline/reconnect-storm and
  default-on needs real measured numbers, so this whole workstream runs after WS-15.

### Endpoint logs (WS-9–13) — edge-first, server-proxied (Netdata-informed)

Adapts Netdata's edge-first logs model to OpenGate's NAT/outbound-only + free-tier reality:
**log-rate signals are centralized in VM (cheap), raw lines stay at the edge and are pulled
on-demand (audited).** Sources: journald/syslog, Windows Event Log, agent self-logs. Centralizing
raw logs into Loki is rejected (Netdata's own argument + the 200 GB cap / 2-OCPU node).

- **WS-9 (agent):** host log readers (no-GPL sourcing — `journalctl -o json` / Windows Event Log
  API) + a rate extractor (per-level, per-unit top-N by rank, volume) feeding the **WS-2 ensemble**.
- **WS-10 (wire):** rate dims reuse **WS-3 `AgentMetricWindow`**; extend `RequestDeviceLogs` for
  host sources; capability-gated; goldens.
- **WS-11 (server):** rate dims → the **WS-4 VM client**; on-demand **raw broker** with **audit
  events** + an **elevated permission**. The central `device_logs` cache was **retired** (option a):
  the broker is truly transient (per-connection single-flight waiter, nothing persisted), so
  "nothing raw centrally persisted" is now structural. See
  `archive/edge-sentinel-ws-11-log-server.md` and ADR-046.
- **WS-12 (web):** logs explorer + log-rate sparkline (WS-6 adapter) + "logs for this anomaly
  window" jump (the metrics↔logs correlation win).
- **WS-13 (privacy/ops):** raw-log redaction corpus; reader-sourcing ADR (no-GPL); Linux + Windows
  reader benchmark before default-on; soak folds into the WS-15b gate.

### Autonomous offline + auto-discovery + threshold alerts (WS-14–19)

Netdata-informed capabilities, adapted to OpenGate's NAT/outbound-only + free-tier reality.

- **Offline operation (WS-14a/14b/15):** each agent runs a **multi-tier persistent TSDB** (T0 1 s /
  T1 1 min / T2 1 hr, ~0.6–1 B/sample target, **anomaly scores stored inline**), holding the
  **min/max/last + 1 s** that aren't sent centrally. **It's a storage-engine project, so WS-14a is a
  spike** (append-only / `redb` / `fjall` / `tsink` / no-persist, measured on a fixture corpus; nothing depends on it
  until it passes) → **WS-14b** builds the winner. It keeps sampling/detecting/alerting **offline**. On
  reconnect, backfill is **resolution-tiered** (recent 24–48 h as 10 s → VM raw; older as
  1 min/1 hr → VM rollups; **1 s never sent** — full-res stays local/on-demand; ~45× less storm
  volume), **hybrid-ordered** (recent-first then drain older), **clamped to VM retention** (≈90 d,
  parameterized — not a hard cap), and governed by a **server-coordinated scheduler** (load-adaptive
  budget, per-tenant fair-share, grant/defer; strict P0 control > P1 live > P2 recent > P3 history).
  Durable local data ⇒ no urgency, no loss; a fleet-wide reconnect drains gradually. **Divergence:**
  Netdata queries each agent **directly** + fans fleet queries out — OpenGate agents are unreachable
  (NAT), so the local TSDB is the **full-res sovereign copy**, a centralized **aggregate** goes to
  VM, and deep/old per-host history is pulled **server-mediated, on-demand**. **Reinstall = a new
  device** (no pre-wipe backfill; old series ages out). **Footprint is the headline risk** — hard
  disk cap (never fills the host), budget asserted in tests; default-off until benchmarked.
- **Auto-discovery & inventory (WS-16/17/18):** non-intrusive, read-only profiling (ports, services,
  DB engines, containers, packages — no WMI, no net scan) → `DiscoveryReport` → a Postgres **RLS**
  inventory table → device-detail view. Generative auto-dashboards are **deferred**.
- **Threshold alerts (WS-19):** declarative edge-evaluated rules (disk<X, CPU>Y sustained) beside the
  ML anomaly; **investigation-aid only** until the FPR soak.
- **Data lifecycle / erasure (WS-20):** deleting a device or a whole tenant from the web client
  triggers an **immediate, irreversible, cascading, idempotent, tenant-scoped purge** across VM
  series, Postgres tables, and cold objects, plus **agent deprovision + local-store wipe** on
  reconnect. A **tombstone/deny-list prevents resurrection** by late/live/offline data (every write
  path rejects tombstoned ids); a **reconciliation GC** catches orphans. The **deletion + completion
  audit events are retained** (erasure proof); tenant-wide purge runs as a resumable background job.
  No soft-delete/undo, auto-expiry, or export this rollout. Backups age out per lifecycle policy
  (can't be surgically purged).

**Dropped (not planned):** eBPF deep monitoring, ransomware detection, and auto-isolation — they
would commit the cross-platform agent to a Linux-only kernel/privilege surface and (for isolation)
autonomous active response; revisit only behind a dedicated privilege/platform ADR.

## Non-functional requirements

- **Security / privacy.** Health rides existing **mTLS QUIC**; **no standing agent→store creds**
  (server is sole writer; PARs only, write-only/short-lived if used). **Full cmdline off by
  default** (CWE-214) — on-demand, audited, elevated, length-capped; redaction = defense-in-depth.
  Numeric tenancy = scoped VM label matcher (single client + cross-tenant-deny tests + CI grep
  gate) — **an application-level guard for internal org segmentation, not a hard boundary for
  mutually-distrusting external tenants** (single-node VM labels *emulate* isolation; hard isolation
  = a future VM-cluster-multitenancy ADR). Process PII in a **Postgres RLS** table (fail-closed) is
  the hard boundary. Numeric labels carry no usernames/paths/secrets, and **per-entity expansion is
  capped** so cardinality can't be driven by host shape.
- **Performance.** Edge ML «1 % CPU, <1 MB RSS (ARM-measured in Wave 0). Detection O(models),
  alloc-free; ring hard-capped; correlation on-demand, bounded. **Control-plane p99 regresses
  ≤ 20 %** under default telemetry (proven in Wave 0 + soak). Range endpoints enforce
  `points ≤ maxPoints`.
- **Maintainability.** **No vendored GPL code.** Agent pure-Rust. New deps reduced vs the original:
  **no Timescale image, no DuckDB/CGO** this rollout; VM is already deployed. Each genuinely-new
  dep + the VM stream-agg config via **ADR review**. Telemetry modules independent of session
  handlers; protocol golden-gated.
- **Operability.** Default-off until Wave 0 + WS-3/WS-4 land + soak passes. Bidirectional
  forward-compatible dispatch + capability negotiation support mixed agent versions. Failure =
  silent degradation. Telemetry stays within VM's existing volume headroom (no new block volume).

## Storage / cardinality (proven in Wave 0; live numbers)

- Live `df`: VM `/storage` = **26 MB / 48.9 GB (0%)**; Loki 1%; Postgres 0%. ~146 GB paid-for
  block storage idle. Space was never the constraint — volume **count** (4/4 at the 200 GB cap)
  and I/O contention were; VM sidesteps both (its own volume, off the control-plane DB).
- **Cardinality — modelled and VM-verified in the Wave 0 spike; real per-host dim counts still owed
  by the WS-2 ARM bench.** The per-agent series count is **host-dependent**: per-core CPU, per-disk,
  per-interface, per-filesystem all multiply the base dims, and in the VM model each aggregate is its
  **own series**. The ingest spike ([`server/tests/vmcardinality`](../../server/tests/vmcardinality/spike_test.go))
  ingested a representative avg-only schema into a real VM and **measured active-series exactly
  matching the model**: a **typical host ≈ 40 series/agent → 20k at 500 agents**, a fully
  per-entity-capped **large host ≈ 99/agent → 49.5k** (both under the **50k** budget); centralising
  all four aggregates would be **~3.5× (71k at 500)**. This confirms the **avg-only-central +
  per-entity-cap** decision keeps central series bounded and ~linear in agent count; min/max/last +
  1 s stay agent-local (WS-14b), sharding cardinality **per host**. **Caveat:** these counts use a
  representative *synthetic* schema — the real base-dim + per-entity numbers are the WS-2 ARM
  sampler bench's Wave 0 deliverable, and the harness re-runs to re-ratify the budget once they land.
- Live `df` earlier showed VM `/storage` ~0% used (~48.9 GB free), so disk headroom exists; the
  binding constraint is the **measured** sample-rate × retention × bytes/sample and query p95 — not
  series count per se (single-node VM handles millions of series).

## Critical files

- Agent: new `mesh-agent-core/src/ml/`, [main.rs](../../agent/crates/mesh-agent/src/main.rs), [session/mod.rs](../../agent/crates/mesh-agent-core/src/session/mod.rs) (bounded-channel precedent)
- Protocol: [control.rs](../../agent/crates/mesh-protocol/src/control.rs), [golden_test.rs](../../agent/crates/mesh-protocol/tests/golden_test.rs); Go [types.go](../../server/internal/protocol/types.go), [golden_test.go](../../server/internal/protocol/golden_test.go)
- Server: [conn.go](../../server/internal/agentapi/conn.go) (dispatch fix + handlers), [auth/auth.go](../../server/internal/auth/auth.go) (OrgID claim), [api/api.go](../../server/internal/api/api.go) (tenant middleware), [device/](../../server/internal/device/) repos (per-tx GUC), [db/migrations/](../../server/internal/db/migrations/) (org/RLS + process tbl), new `internal/telemetry/` (VM client + scoped reader), new `internal/correlate/`
- API/UI: [api/openapi.yaml](../../api/openapi.yaml), [web/package.json](../../web/package.json), [DeviceList.tsx](../../web/src/features/devices/DeviceList.tsx), new device-detail metrics view
- Ops: [deploy/grafana/provisioning/dashboards/](../../deploy/grafana/provisioning/dashboards/), [deploy/helm/monitoring/](../../deploy/helm/monitoring/) (VM stream-agg + retention + storage bump), [loadtest](../../server/tests/loadtest/main.go)

## Shared conventions (apply to every WS — see [CLAUDE.md](../../CLAUDE.md))

- **TDD, no bypass.** Failing test before source; positive + negative. No
  `t.Skip`/`.skip`/`#[ignore]` — tests always run deterministically.
- **Per WS:** `/precommit` (`scripts/precommit-gauntlet.sh`) before every commit → commit →
  `/refactor` → push to `dev` only. Author = Ivan Volchanskyi, no co-authors.
- **Protocol:** any `ControlMessage` change is golden-gated (`make golden`).
- **No GPL** vendored/ported (Netdata GPL-3; workspace Apache-2). Clean-room only.
- **New deps** (`uplot`, future storage/query substrates) each need an **ADR** in [docs/adr/](../../docs/adr/) +
  index row in [decisions.md](../decisions.md). The VM stream-agg/server-ingest decision is
  recorded in [ADR-044](../../docs/adr/ADR-044-edge-sentinel-server-telemetry-ingest.md). **Not** introduced this rollout: Timescale image,
  DuckDB/CGO, `arrow`/`parquet` (only if optional archival is built).
- **Default-off** behind a flag until Wave 0 + WS-3/WS-4 land + soak passes; failure = silent degradation.
- **Tenancy:** after WS-0 every new tenant table carries `org_id` + RLS; every new repo method runs
  in a tenant-scoped tx; numeric VM reads go through the scoped client; add a cross-tenant-deny test.
- **Docs:** end each WS with a [/docs](../../docs/) update (link, don't paraphrase).

## Reviewer checklist template (Claude reviews each implementation)

- [ ] Failing test existed before source (TDD); positive + negative covered.
- [ ] Scope matches this WS only; no unrelated churn; no silent SKIP.
- [ ] Repo rules: no GPL, no Sonar/lint suppressions, golden updated if protocol touched.
- [ ] Tenancy: org_id + RLS (Postgres) / scoped label (VM) + cross-tenant-deny test where applicable.
- [ ] Security/NFR: no secrets/PII leak; full cmdline not stored by default; bounded resources; mTLS-only transport; no standing creds.
- [ ] `/precommit` green; `/docs` updated.

## Verification

- **Rust:** `cargo test -p mesh-agent-core` (detector/sampler/ring, on-demand redaction); ARM bench (Wave 0).
- **Cross-language:** `make golden` for new variants; bidirectional forward-compat + capability fixtures.
- **Server:** Go handler/store tests; migrations in `testpg`; **RLS cross-tenant-deny** (process table);
  VM label-scope deny; correlation ranking (injected anomaly ranks #1).
- **Load (Wave 0 + WS-15b):** VM ingest spike then sustained soak → control-plane p99, VM ingest/
  cardinality/disk growth, correlation latency recorded; default-on only if budgets pass.
- **Web/E2E:** `make e2e` (badge, panel, uPlot, drill-down); `npm run size` (chunk budget).
- **Manual:** `/run` stack, inject CPU/disk load + a secret-bearing process, confirm bit flips,
  correlation ranks it, process stored as basename+hash, full cmdline only via audited on-demand,
  and tenant scoping hides it cross-tenant (RLS in Postgres, label matcher in VM).
- **Gate:** `make lint`, `make sonar`, full `/precommit`; `/refactor` before push.

## Housekeeping

- ADRs: (a) edge-first + clean-room ML, (b) multi-tenant RLS, (c) **telemetry storage =
  VictoriaMetrics OSS + stream-aggregation** ([ADR-044](../../docs/adr/ADR-044-edge-sentinel-server-telemetry-ingest.md)),
  (d) process-telemetry PII (basename+hash default;
  on-demand full cmdline), (e) protocol capability negotiation, (f) charting/bundle budget,
  (g) endpoint-log model (edge-stored, server-proxied; why not Loki), (h) raw-log privacy
  (no central storage; audited on-demand; elevated permission), (i) journald/Windows reader
  sourcing (no-GPL), (j) agent local TSDB substrate **chosen by the WS-14a spike** (append-only /
  redb / fjall / tsink / no-persist; permissive-license + embeddable + pure-Rust rubric;
  footprint/write-amp/CPU-predictability/maturity gates) + backfill (import-API not stream-agg; NAT
  divergence from direct/distributed query), (k) auto-discovery/inventory model,
  (l) edge threshold-alert rules, (m) data-lifecycle/erasure (cascading device/tenant purge across
  VM + Postgres + cold + agent; audit retained) — index rows in
  [.claude/decisions.md](../decisions.md). Update [.claude/phases.md](../phases.md).
- New deps via ADR review (`uplot`, VM stream-agg config). `/docs` update each workstream.
