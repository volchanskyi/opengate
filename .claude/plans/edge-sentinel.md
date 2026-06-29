# Edge-Sentinel — Netdata-informed local anomaly detection for the Rust agent

> **Status:** Planning / research proposal — to be revisited. No code until a phase
> is green-lit; every runtime behavior ships behind a **default-off** flag and
> degrades silently (the sentinel must never block remote management).
>
> This is the single canonical plan. It consolidates and supersedes the three
> drafts `edge-sentinel-telemetry.md`, `edge-first-sentinel.md`, and
> `edge-sentinel-ml-evaluation.md` (deleted on adoption of this file).

## Context

The Rust agent is **reactive**: it registers, heartbeats, accepts sessions, applies
updates, and collects hardware/logs **on request**. Goal: evolve it into an **edge
sentinel** — profile host health locally, detect anomalies with no labeled data
(Netdata's unsupervised, zero-config approach), and surface low-cardinality insight
to the server — **without** turning the OKE Always-Free deployment into a paid
analytics stack.

A third-party architect proposed a 5-phase design (ClickHouse/TimescaleDB sidecar,
custom Go correlation engine, 20k-host heatmap). Cross-checking it against the real
codebase surfaced **fatal mismatches**, all from missing context:

1. **Redundant storage.** VictoriaMetrics + Grafana + Loki + node/pg exporters are
   already in prod ([deploy/helm/monitoring/values.yaml](../../deploy/helm/monitoring/values.yaml),
   [ADR-038](../../docs/adr/ADR-038-victoriametrics-ci-trend-store.md)). ClickHouse/
   TimescaleDB as always-on StatefulSets don't fit and aren't needed.
2. **Phantom scale.** Sized for "20k concurrent endpoints"; proven scale is
   single-node / 1 replica / ~1 agent, load-tested ~100–500
   ([docs/Multiscale-Readiness.md](../../docs/Multiscale-Readiness.md),
   [docs/Testing.md](../../docs/Testing.md)). Multi-replica is a **pending rebuild**
   ([ADR-023](../../docs/adr/ADR-023-relay-extraction-redis-session-registry.md)).
3. **Protocol misread.** A new `ControlMessage` is **additive** (golden Rust↔Go
   compat tests exist) — not a "breaking upgrade." Option-B's "dedicated QUIC stream"
   collides with the single-control-stream / stream-ownership workaround
   (ADR-005/ADR-037).
4. **Category error (vs Netdata).** The architect adopted Netdata's *ML* while
   discarding its *architectural core*: **every agent is a store-and-query engine at
   the edge; the center receives signal on-demand, not a firehose.** Re-centralizing
   raw storage onto the one free-tier server is the opposite of edge-first.

The well-motivated kernel: **edge-local scoring** (ship compact summaries) — agents
are **outbound-only behind NAT** (can't be scraped like node-exporter) and the
**200 GB OCI block cap is fully consumed**, so raw centralization is doubly penalized.

### Decisions locked

| Topic | Decision | Why |
|---|---|---|
| Edge intelligence | **Clean-room k=2 k-means ensemble** (Netdata shape: up to 18 staggered models/metric, consensus voting, **99th-percentile** training-distance threshold) | Faithful to Netdata's unsupervised engine; capability built now, model count configurable, **measured on ARM before default-on** |
| License | **Clean-room only — no vendored/ported Netdata code** | Netdata is **GPL-3.0-or-later**; this workspace is **Apache-2.0** ([agent/Cargo.toml](../../agent/Cargo.toml)) — porting GPL code would relicense the agent |
| Storage | **Edge ring-buffer (Tier 0, RAM) → summaries to VictoriaMetrics + latest state to Postgres → on-demand pull → (gated) Parquet cold tier queried by server-side embedded OLAP** | Mirrors Netdata's parent-child/RAM-child model; keeps Always-Free footprint intact; gives a real path to history without a new always-on DB |
| ClickHouse/Timescale (hosted) | **Rejected for free tier** | No durable fit (4/4 block volumes used, 12 GB RAM node, 20 GB/50k-req object storage). Hosted DB deferred until off free tier |
| UI | **Health badge + anomaly panel first; native React charting (uPlot) for timelines** | Cheapest operator value first; user chose native charting for the dense view |
| Scope | **Design now, build incrementally** at proven scale; cardinality fan-out gated behind the multiscale rebuild | Delivers on today's single-replica path without blocking on ADR-023 |

## Netdata alignment

**ML kernel (clean-room replication of the *design*, not the *code*):** per-metric
k=2 k-means; multiple staggered models trained on rolling windows (Netdata shape ≈18
models, ~6 h each, 3 h stagger → ~54 h); feature vectors are **lagged windows**
(current + N preceding samples) so *shape* anomalies are caught, not just level; the
**anomaly bit is set only on consensus** (all trained models agree the sample exceeds
their **99th-percentile** training-distance boundary × a guard-band) — consensus +
percentile are Netdata's false-positive controls (the architect's `max×1.5` is a blind
spot: one noisy training sample inflates `max`).

**DBENGINE parent-child mapping — Netdata's own model validates this design:**
Netdata's blog states children can run **RAM-mode** while **parents** persist via
`dbengine`. That is exactly our split:

| Netdata DBENGINE | OpenGate equivalent |
|---|---|
| **RAM-mode child** (Tier 0 hot pages, no disk) | **Agent in-RAM ring buffer** feeding the ensemble |
| **Parent** persisting via dbengine (append-only ring eviction of oldest datafile) | **Server → VictoriaMetrics** (retention-based eviction = same "ring on disk") |
| Parent-child **streaming** | Agent→server summary over the existing QUIC control stream |
| **Multi-tier downsampling** (T0 1 s ≈0.6 B/sample → T1 1 min → T2 1 hr) | **Split across the wire** (see below) — VM-enterprise downsampling or Timescale continuous aggregates only if 20k forces it |

So: **VictoriaMetrics plays the parent-dbengine role; the agent RAM buffer plays the
RAM-mode child.** Among hosted engines, VM is closest to DBENGINE's storage internals
(purpose-built compressed TSDB, quota eviction); Timescale **continuous aggregates**
(OSS) are the closest match to DBENGINE's *tiered downsampling* (VM downsampling is
enterprise-only).

## Target architecture (edge-first)

```
 Managed host (spare capacity)                      OKE Always-Free (≈2 OCPU/12 GB, 1 node)
┌──────────────────────────────────────┐          ┌──────────────────────────────┐
│ sampler → ring-buffer (tiered) ─┐     │          │ control handler              │
│   sysinfo   bounded, disk-capped │     │  mTLS    │  ├ latest state → Postgres   │
│                                  ▼     │  QUIC    │  └ (opt) anomaly-rate → VM    │
│ detector: k=2 ▸ consensus ▸ rate ──────┼─────────▶│ web: health badge + panel    │
│   (99th-pct threshold)                 │ summary  │       + uPlot timeline        │
│                                  ▲     │ + on-    │ (later) on-demand OLAP:       │
│ Parquet flush ──┐                │     │ demand   │  DuckDB/chDB over Parquet     │
└─────────────────┼────────────────┘     │ pull     └─────────────┬────────────────┘
                  ▼ pre-signed PUT                                 │ range reads
            OCI Object Storage (20 GB free, Parquet) ◀────────────┘
```

Edge does detection **and** keeps recent history; the server gets a compact summary
each heartbeat and can **pull** a bounded window on demand (Netdata-style replication
over the existing control plane). The object-storage cold tier is a later, gated phase.

## Storage & retention strategy (DBENGINE tiering, split across the wire)

Emulate DBENGINE's tiers across the agent↔server boundary so the firehose never crosses
the NAT:

- **Tier 0** = agent RAM, 1 s resolution, **ephemeral**, sole consumer is the ensemble.
  Footprint ~tens of KB (e.g. 20 dims × 900 samples × 4 B ≈ 72 KB) — far below DBENGINE's
  per-node cost because no persistent tiers live on the edge.
- **Tier 1** = what crosses the wire: pre-downsampled 10–60 s windows (min/max/avg/last)
  + anomaly bitmask → **VictoriaMetrics** (30 d); **latest state** → **Postgres** (for
  UI/JOINs/alerts). Agent does the T0→T1 downsample, so central storage only holds
  reduced data.
- **Tier 2** = hourly rollups, **only if 20k forces it**: VM-enterprise downsampling or
  TimescaleDB continuous aggregates (OSS).
- **Cold/historical** = agent flushes **Parquet** to **OCI Object Storage** (Always-Free
  20 GB / 50k req/mo) via **server-issued pre-signed PAR URLs** (no standing object-store
  creds at the edge); queried **on-demand** by a **server-side embedded OLAP**
  (DuckDB/MIT or chDB/Apache-2.0 — license-compatible, **never shipped in the agent**).
  This is *where ClickHouse-class analytics actually fit on free tier*: embed the query
  engine, don't host a DB.

**Per-scale storage math (why tiering is a 20k concern, not a now concern):** ~20 dims
shipped as 10 s aggregates ≈ **0.7 MB/day/agent** in VM.
- 1–500 agents → ~10 GB / 30 d → **fits the ~40 GB headroom** under VM's 50 GB OCI floor
  (chart requests only 10 GiB, [values.yaml:26](../../deploy/helm/monitoring/values.yaml#L26)).
  **Flat retention is fine.**
- 20k agents → **~14 GB/day** → fills headroom in ~3 days → **must** downsample (Tier 2)
  or push coarser aggregation to the edge (60 s windows). Decision deferred to that scale.

**Hosted ClickHouse free-tier verdict (recorded):** no durable fit — every path is
ephemeral (emptyDir), quota-busting (S3-disk vs 50k req/mo), or evicts an existing store;
defer until off free tier.

## Phased rollout (P0–P7)

Each phase: independently shippable, **default-off**, gated on the prior phase's measured
acceptance. Per phase: TDD → `make golden`/tests → `/precommit` → commit → `/refactor` → push.

- **P0 — Detector kernel (clean-room, pure Rust).** New `mesh-agent-core` module:
  `DetectorConfig`, `KMeansModel` (k=2), `EdgeMlEnsemble` (consensus via `minimum_votes`),
  `AnomalyRateWindow` (bit-packed). 99th-pct boundary × guard-band. No task/protocol/infra.
  *Accept:* `cargo test -p mesh-agent-core` (positive + negative: percentile robustness,
  non-finite rejection, consensus, window roll/pack).
- **P1 — Host sampler.** `MetricSampler` trait; binary impl uses **`sysinfo` (already a
  workspace dep)** — reuse `collect_hardware_info()` patterns
  ([main.rs:739](../../agent/crates/mesh-agent/src/main.rs#L739)). `FakeSampler` for tests.
  CPU%, mem, disk per mount, net counters; **no root-only collectors**; bounded interval.
  *Accept:* deterministic fake-sampler tests; one-shot dump.
- **P2 — Edge ring-buffer + tiered downsample.** Bounded RAM ring + minute/hour tiers,
  **hard RAM/disk cap** (reject growth). Optional disk spill via `arrow`/`parquet`
  (Apache-2.0) — **new deps → ADR review** ([rules/code.md](../rules/code.md)).
  *Accept:* property tests for ring/rollover/eviction; RSS + cap assertions.
- **P3 — Training scheduler.** Staggered windows from the ring; **start with far fewer
  than 18 models, measure CPU/RSS on A1-class ARM**, then scale toward the full ensemble.
  Training **yields** to session/update/control traffic; detection stays O(models),
  alloc-free post-load. *Accept:* ARM bench; model-budget cap enforced.
- **P4 — Wire contract (summary + on-demand pull).** Extend `ControlMessage`
  ([control.rs](../../agent/crates/mesh-protocol/src/control.rs) — `#[non_exhaustive]`,
  `#[serde(tag="type")]`; precedent `HardwareReport`); emit from the 60 s heartbeat loop
  ([main.rs ~L561](../../agent/crates/mesh-agent/src/main.rs#L561)). Reuse `FRAME_CONTROL`,
  **no new QUIC stream**.
  - `AgentHealthSummary { ts, node_anomaly_rate, per_family_rates, recent_bitmask, sampler_ver, model_ver }` — bounded, interval floor.
  - `RequestHealthWindow` / `HealthWindowResponse` — server pulls a **bounded** recent
    window from the ring buffer on demand (drill-down without central storage).
  - **Rust-generated + Go-verified goldens**
    ([protocol/golden_test.go](../../server/internal/protocol/golden_test.go)); server
    ignores unknown summaries (mixed-version rollout). *Accept:* `make golden`; round-trips.
- **P5 — Server persistence + UI (first surface).** Latest state per device in Postgres
  ([device/postgres.go](../../server/internal/device/postgres.go) + migration) — no new DB.
  Optional **low-cardinality** anomaly-rate → VM **after a cardinality estimate** (clamp to
  `device_id`; no per-process/path labels). Web: device-list **health badge** +
  device-detail **anomaly panel**. *Accept:* handler/store tests (`testpg`); `make e2e`;
  documented VM cardinality estimate.
- **P6 — Native React timeline (uPlot).** Add **uPlot** (canvas; tens of thousands of
  points at 60 fps; Recharts as simpler-DX fallback) to
  [web/package.json](../../web/package.json); device-detail metrics timeline backed by a
  new REST VM range-query proxy. Vendor stylesheet imported as a vendor asset (respects the
  Tailwind-only rule). *Accept:* `make e2e` for the chart; regen API both sides
  (`oapi-codegen`; `npm run generate:api`).
- **P7 — Cold tier + on-demand OLAP (gated on real need).** Agent flushes Parquet to OCI
  Object Storage via **server-issued PAR URLs**; **server/operator-side** DuckDB/chDB
  range-reads on demand (engine **never** in the agent). Grafana dashboard + Telegram alert
  via the **existing** path. *Accept:* PAR issue/expiry tests; query smoke over sample
  Parquet; stays within 20 GB / 50k-req budget.

**Multiscale gate:** validate cardinality/VM RAM at load-test scale (500 agents) before
fan-out beyond ~1k; full 20k validation is deferred behind the ADR-023 registry rebuild.

## Non-functional requirements

- **Performance.** Edge ML is cheap (Netdata: ~18 KB/model, 2–4% of one core per *10k*
  metrics) → our ~10–25 dims = «1% CPU, <1 MB RSS, **on the managed host, not the A1
  server**. Detection O(models), alloc-free post-load; ring hard-capped; server ingest
  bounded by interval + batch. **Measure, don't assume** (ARM bench in P3).
- **Security.** Health rides the existing **mTLS QUIC** control plane — **no standing
  agent→VM or agent→object-store creds**; object uploads use short-lived pre-signed URLs.
  Metric names/labels/Parquet columns must not leak usernames, paths, process command
  lines, peer IPs, or secrets. Numeric aggregates only; config-gated; documented.
- **Maintainability.** **No vendored GPL code (clean-room ML).** Embedded OLAP is
  server-side only; the agent stays pure-Rust. New deps (`arrow`/`parquet`, object-store
  client, DuckDB/chDB) each require **ADR review**. Telemetry modules independent of
  session handlers; protocol changes golden-gated.
- **Operability.** Default-off until P4/P5 land. Mixed agent versions supported. Failure =
  silent degradation. Object/VM history is on-demand/bounded, never always-on.

## Open decisions / clarifying questions (recommendations; confirm on revisit)

1. **First metric families** → *Rec:* CPU + memory (highest-signal, unprivileged,
   deterministic cross-platform); disk/net next; process/service later (PII-sensitive).
2. **Storage reach for the first slice** → *Rec:* P2 edge ring + P4 on-demand pull; treat
   P7 Parquet/OLAP as gated on a measured incident workflow.
3. **Fleet/cardinality budget** → *Rec:* design for **tens** now (matches single-replica
   prod) with strict per-device label caps; thousands forces summary-only + Parquet from
   day one.
4. **Signal action** → *Rec:* investigation-aid only first (badge + panel); wire the
   existing `Notifier`/Telegram path only after a false-positive soak.
5. **Visibility** → admins only, or any operator who can view the device?
6. **OCI envelope nuance** → deployed node is **2 OCPU / 12 GB** (binding for co-tenancy);
   whether the tenancy may grow to 4/24 is contested (repo terraform asserts a 4/24 cap;
   current Oracle Always-Free docs indicate 2/12) — operator to verify. The 12 GB co-tenancy
   squeeze holds either way.

## Critical files

- Agent: new `mesh-agent-core/src/ml/` (detector/sampler/ring), [main.rs](../../agent/crates/mesh-agent/src/main.rs) (loop + `collect_hardware_info`), [session/mod.rs](../../agent/crates/mesh-agent-core/src/session/mod.rs) (bounded-channel precedent)
- Protocol: [mesh-protocol/src/control.rs](../../agent/crates/mesh-protocol/src/control.rs), [tests/golden_test.rs](../../agent/crates/mesh-protocol/tests/golden_test.rs); Go [protocol/types.go](../../server/internal/protocol/types.go), [protocol/golden_test.go](../../server/internal/protocol/golden_test.go)
- Server: [agentapi/conn.go](../../server/internal/agentapi/conn.go), [device/postgres.go](../../server/internal/device/postgres.go), [db/migrations/](../../server/internal/db/migrations/), new `internal/vmingest/`, (P7) new `internal/olap/`
- API/UI: [api/openapi.yaml](../../api/openapi.yaml), [web/package.json](../../web/package.json), [web/src/features/devices/DeviceList.tsx](../../web/src/features/devices/DeviceList.tsx), new device-detail metrics view
- Ops: [deploy/grafana/provisioning/dashboards/](../../deploy/grafana/provisioning/dashboards/), [deploy/helm/monitoring/values.yaml](../../deploy/helm/monitoring/values.yaml)

## Verification

- **Rust:** `cargo test -p mesh-agent-core` (detector/sampler/ring, positive + negative);
  ARM bench for P3 budgets; RSS/CPU footprint measured, not assumed.
- **Cross-language:** `make golden` for new `ControlMessage` variants; round-trips both langs.
- **Server:** Go handler/store table tests; migration applied in `testpg`; VM push tested
  (testcontainers/stub); `device_anomaly_events` rows on onset/clear.
- **Load:** extend [loadtest](../../server/tests/loadtest/main.go) to 500 agents emitting
  health; confirm VM ingest + server CPU bounded; record VM active-series count.
- **Web/E2E:** `make e2e` for badge + panel (P5) and the uPlot timeline (P6).
- **Manual:** `/run` the stack, inject CPU load, watch the bit flip in the UI and (P7) a
  Telegram alert fire.
- **Gate:** `make lint`, `make sonar`, full `/precommit`; `/refactor` before push.

## Housekeeping on execution

- Update [.claude/phases.md](../phases.md) (In-Progress row) and add an ADR in
  [docs/adr/](../../docs/adr/) for the edge-first storage + clean-room-ML decision (index
  row in [.claude/decisions.md](../decisions.md)).
- New crates/deps (`arrow`/`parquet`, object-store, DuckDB/chDB) land via ADR review.
- `/docs` update at each implementation phase (canonical developer docs).
