# Edge-Sentinel — Netdata-informed local anomaly detection + multi-tenant RMM telemetry

> **Status:** Finalized planning — single rollout, **all workstreams built together**,
> dependency-ordered, **no approval gates / no spike-then-wait**. Runtime behavior ships
> behind a default-off flag and degrades silently (the sentinel must never block remote
> management). Supersedes the deleted drafts.

## Context

Evolve the reactive Rust agent into an **edge sentinel** — profile host health locally,
detect anomalies with no labeled data (Netdata's unsupervised, zero-config engine), and
surface insight to the server — framed for OpenGate's domain: a **multi-tenant RMM**.

A third-party architect reviewed the prior plan and found three **confirmed** gaps
(verified against code this session):

1. **Protocol compat was overstated.** Unknown control type → `ErrUnexpectedMessage`
   ([conn.go:245](../../server/internal/agentapi/conn.go#L245)) → control loop drops the
   connection ([server.go:260](../../server/internal/agentapi/server.go#L260)), pinned by
   [conn_test.go:612](../../server/internal/agentapi/conn_test.go#L612). Additive is
   wire-additive, **not operationally safe** until dispatch is made forward-compatible.
2. **Tenancy is a product migration, not a label.** Confirmed **zero** `org_id`/tenant/RLS
   anywhere ([001_initial.up.sql](../../server/internal/db/migrations/001_initial.up.sql);
   no matches in server Go). Today's authz is `users.is_admin` + `security_groups` RBAC.
3. **Storage math must be measured.** 20 dims × **4** aggregates × (86400/10) × 4 B =
   **2.64 MiB/day/agent raw** → **38.6 GiB / 500 agents / 30 d**. Compression and **active-
   series cardinality** swing this several-fold → measured in load-test, not estimated.

### Decisions locked (this session)

| Topic | Decision |
|---|---|
| Edge intelligence | Clean-room **k=2 k-means ensemble** (Netdata shape: ~18 staggered models/metric, consensus, **99th-pct** threshold), ARM-measured before default-on |
| License | **Clean-room only** — no vendored/ported Netdata code (GPL-3 vs workspace Apache-2, [agent/Cargo.toml](../../agent/Cargo.toml)) |
| **Tenancy** | **Full product-wide multi-tenancy + Postgres RLS now** (WS-0 foundation) |
| **Hot store** | **TimescaleDB extension on the existing Postgres 17** (hypertable + compression + continuous aggregates + retention) |
| **Aggregates** | **Full {min,max,avg,last} @10 s** |
| **Metric families** | CPU + memory + disk + network + **process/service** |
| **Process PII** | **Full name + cmdline**, with **on-by-default secret-redaction** of argv; descriptive fields in an RLS relational table; only numeric per-**top-N-rank** series in the hypertable (bounds cardinality) |
| Correlation | **On-demand**, native SQL in Timescale (RLS-scoped), KS-test + anomaly-rate ranking; bounded concurrency/timeout |
| Cold tier | **Parquet → OCI Object Storage** (tenant-partitioned, server-issued PARs), **DuckDB** on-demand for long-term/compliance + chunk offload to keep the shared Postgres lean |
| Signal action | **Investigation-aid only first** (no auto-notify until FPR soak) |
| Visibility | **Any device-viewer in the org** |
| Protocol | **Forward-compat dispatch fix** (log-and-continue on unknown agent→server types) + flip the pinning test |

### Headline risk (accepted, mitigated, measured)

Timescale-on-the-existing-Postgres + full aggregates @10 s + all families incl. process is
the **highest-load path**, and free-tier has **no spare volume** (4/4 used), so telemetry
**shares the Postgres that runs live relay/WebRTC/device control**. Mitigations are
**mandatory, not optional**: native compression + continuous aggregates + retention +
cold-chunk offload to Parquet, a measured **control-plane p99 budget**, and a documented
**mitigation ladder** (coarsen interval → drop to 2 aggregates → isolate Timescale to its
own instance). The ladder is a measurement-triggered contingency, **not** a build gate.

## Architecture

```
 Managed host                                       OKE Always-Free (≈2 OCPU/12 GB, 1 node)
┌──────────────────────────────────────┐          ┌───────────────────────────────────────┐
│ sampler(cpu/mem/disk/net/proc) → ring │  mTLS    │ control handler (forward-compatible)   │
│   bounded RAM, 1s (Tier 0, ephemeral) │  QUIC    │  ├ inventory+health → Postgres (RLS)   │
│ detector: k=2 ▸ consensus ▸ 99th-pct  ├─────────▶│  ├ numeric series → Timescale hypertbl │
│   per-family + node anomaly rate      │ summary  │  └ proc name+cmdline(redacted) → RLS tbl│
│ + org_id; cmdline redaction at source │ + pull   │ on-demand correlation (SQL, RLS)       │
│ Parquet flush (tenant-partitioned) ───┼──────────│ web: badge + panel + uPlot + drilldown │
└───────────────────────────────┬───────┘ PAR PUT │ DuckDB over Parquet (cold/compliance)  │
                                 ▼                  └───────────────────────────────────────┘
            OCI Object Storage (20 GB free, Parquet, tenant-partitioned)
```

DBENGINE tiering split across the wire: **T0** = agent RAM (1 s, ephemeral, ML only);
**T1** = Timescale hypertable (compressed, continuous-aggregate rollups); **cold** =
Parquet/object (long retention, DuckDB on-demand). Agents are outbound-only (NAT), so the
server is the only writer to all stores; no standing agent credentials.

## Workstreams (one rollout, dependency-ordered)

Each: **TDD first**, then `make golden`/tests, `/precommit`, commit, `/refactor`, push.

### WS-0 — Multi-tenancy foundation + RLS (prerequisite)
- New `organizations` table; add `org_id UUID NOT NULL` to every tenant table (users,
  groups_, devices, agent_sessions, audit_events, amt_devices, enrollment_tokens,
  security_groups, device_* tables) via migration; **backfill** all existing rows to a
  seeded default org.
- **RLS** `ENABLE`/`FORCE` + policies `USING (org_id = current_setting('app.current_org')::uuid)`;
  `is_admin` → cross-org bypass (BYPASSRLS role or policy exception).
- Add `OrgID` to JWT `Claims` ([auth.go:21](../../server/internal/auth/auth.go#L21)); a
  **tenant-context middleware** after auth in the authenticated chi group
  ([api.go:277](../../server/internal/api/api.go#L277)); thread **per-transaction
  `SET LOCAL app.current_org`** through the repository layer (queries run inside a
  tenant-scoped tx — the invasive part).
- Web: org context + (multi-org users) switcher.
- *Tests:* **cross-tenant-deny** suite (every repo); middleware sets/clears GUC; admin bypass.
- *ADR:* multi-tenant RLS model.

### WS-1 — Protocol forward-compatibility
- Change agent→server dispatch `default:` to **log-and-continue** (return nil) for
  unrecognized types ([conn.go:245](../../server/internal/agentapi/conn.go#L245)); flip
  `TestAgentConn_HandleUnknownMessage` to assert no-error (test-first).

### WS-2 — Edge ML + sampler (agent, clean-room, pure Rust)
- New `mesh-agent-core/src/ml/`: `KMeansModel` (k=2), `EdgeMlEnsemble` (consensus,
  99th-pct × guard-band), `AnomalyRateWindow` (bit-packed). Staggered training, **yields**
  to session/control traffic; detection O(models), alloc-free post-load; **ARM bench**,
  model-budget cap.
- `MetricSampler` trait; binary impl reuses **`sysinfo` (already a dep)** —
  ([main.rs:739](../../agent/crates/mesh-agent/src/main.rs#L739)). Families: CPU/mem/disk/net
  + **process/service top-N by rank** (numeric cpu%/mem per rank). **cmdline secret-
  redaction at source** (on by default). Bounded ring buffer, hard RAM/disk cap.
- *Tests:* `cargo test -p mesh-agent-core` (consensus, percentile robustness, non-finite
  rejection, ring rollover/eviction, **redaction unit tests** for known secret patterns).

### WS-3 — Wire contract (additive, tenant-tagged, golden-gated)
- Additive `ControlMessage` variants ([control.rs](../../agent/crates/mesh-protocol/src/control.rs),
  `#[non_exhaustive]`), emitted from the 60 s heartbeat loop, reusing `FRAME_CONTROL`
  (**no new QUIC stream**):
  - `AgentHealthSummary { ts, org_id, node_anomaly_rate, per_family_rates, recent_bitmask, sampler_ver, model_ver }`
  - `AgentMetricWindow { ts, org_id, per-dim {min,max,avg,last} @10 s }`
  - `ProcessReport { ts, org_id, top_n: [{rank, name, cmdline(redacted), pid, cpu, mem}] }`
  - `RequestHealthWindow` / `HealthWindowResponse` (bounded on-demand pull)
- **Rust→Go goldens** ([golden_test.go](../../server/internal/protocol/golden_test.go)).
- *Tests:* `make golden`; round-trips both languages.

### WS-4 — Server ingest + TimescaleDB
- Migration: enable `timescaledb`; `device_metrics` **hypertable** (numeric, `org_id` +
  RLS), **compression** + **continuous aggregates** (1 min/1 hr rollups) + **retention**;
  `device_processes` RLS relational table (name, **redacted** cmdline, pid, rank).
- Handlers (mirror `handleHardwareReport`) for the new messages → tenant-scoped writes;
  server-side **redaction guard** (defense-in-depth even if agent redaction is off).
- *Tests:* Go handler/store (`testpg`), RLS deny, compression/CA policy applied.

### WS-5 — Correlation engine (on-demand, Netdata Anomaly-Advisor)
- `internal/correlate` + REST endpoint ([api/openapi.yaml](../../api/openapi.yaml)): given
  window + tenant, rank top-N dimensions (KS-test two-sample + anomaly-rate volume) via
  **native SQL on the hypertable** (RLS auto-scopes); bounded concurrency + timeout.
- *Tests:* injected anomaly ranks #1; tenant-scope enforced; timeout/concurrency bounds.

### WS-6 — Web UI (native React)
- Health **badge** on the virtualized device grid
  ([DeviceList.tsx](../../web/src/features/devices/DeviceList.tsx)); device-detail
  **anomaly panel** + **uPlot** timelines (canvas) + **correlation drill-down** (window
  select → top-N). Add `uplot` to [web/package.json](../../web/package.json) (vendor CSS as
  a vendor asset). org-scoped via existing auth.
- *Tests:* `make e2e` (badge, panel, timeline, drill-down for a seeded anomalous device).

### WS-7 — Cold tier + DuckDB OLAP
- Agent flushes **tenant-partitioned Parquet** to OCI Object Storage via **server-issued
  PARs** (no standing creds); Timescale **chunk offload** of aged data → Parquet to keep
  the shared Postgres lean. **Server-side** DuckDB `postgres_scanner` for historical +
  cross-fleet relational correlation (engine **never** in the agent).
- *Tests:* PAR issue/expiry; query smoke over sample Parquet; stays within 20 GB / 50k req.

### WS-8 — Ops + measurement (informs tuning, not a gate)
- Grafana dashboard (Postgres/Timescale datasource). Alerts **deferred** (investigation-aid
  only). Extend [loadtest](../../server/tests/loadtest/main.go) to **500 multi-tenant
  agents** with full telemetry; **measure**: control-plane p99 under load, ingest/
  compression ratio, active-series cardinality, correlation latency, RLS overhead. Apply
  the **mitigation ladder** if the p99 budget is exceeded.

## Non-functional requirements

- **Security / privacy.** Health rides existing **mTLS QUIC**; **no standing agent→store or
  agent→object creds** (PARs only). **cmdline argv secret-redaction** on by default
  (agent + server defense-in-depth) — argv routinely carries credentials and health is
  visible to any org device-viewer. Descriptive process data in an **RLS relational table**;
  numeric only in the hypertable. **RLS cross-tenant-deny tested** on every repo. Numeric
  metric labels carry no usernames/paths/secrets.
- **Performance.** Edge ML «1 % CPU, <1 MB RSS, on the managed host (ARM-measured).
  Detection O(models), alloc-free; ring hard-capped; correlation **on-demand only**.
  **Control-plane p99 budget** enforced via the measured load-test + mitigation ladder.
- **Maintainability.** **No vendored GPL code.** Embedded OLAP server-side only; agent
  pure-Rust. New deps (`arrow`/`parquet`, object-store, **DuckDB — CGO**) + the TimescaleDB
  custom image each via **ADR review**. Telemetry modules independent of session handlers;
  protocol golden-gated.
- **Operability.** Default-off until WS-3/WS-4 land. Forward-compatible dispatch supports
  mixed agent versions. Failure = silent degradation.

## Storage / cardinality (measured in WS-8)

- Raw: ~25 base dims × 4 aggregates @10 s = ~2.6 MiB/day/agent **before** process metrics;
  process adds top-N-rank numeric series (bounded) + a relational row set (not TSDB labels).
- Timescale native compression (~10–20× on time-series) + continuous-aggregate rollups +
  retention + cold-chunk offload keep the **shared 50 GB Postgres PVC** lean.
- Real 20k limiter is **active-series cardinality** (4 aggregates × dims × agents); process
  bounded by rank avoids explosion. Exact numbers come from the WS-8 load-test.

## Critical files

- Agent: new `mesh-agent-core/src/ml/`, [main.rs](../../agent/crates/mesh-agent/src/main.rs), [session/mod.rs](../../agent/crates/mesh-agent-core/src/session/mod.rs) (bounded-channel precedent)
- Protocol: [control.rs](../../agent/crates/mesh-protocol/src/control.rs), [golden_test.rs](../../agent/crates/mesh-protocol/tests/golden_test.rs); Go [types.go](../../server/internal/protocol/types.go), [golden_test.go](../../server/internal/protocol/golden_test.go)
- Server: [conn.go](../../server/internal/agentapi/conn.go) (dispatch fix + handlers), [auth/auth.go](../../server/internal/auth/auth.go) (OrgID claim), [api/api.go](../../server/internal/api/api.go) (tenant middleware), [device/](../../server/internal/device/) repos (per-tx GUC), [db/migrations/](../../server/internal/db/migrations/) (org/RLS + timescale + process tbl), new `internal/correlate/`, `internal/olap/`
- API/UI: [api/openapi.yaml](../../api/openapi.yaml), [web/package.json](../../web/package.json), [DeviceList.tsx](../../web/src/features/devices/DeviceList.tsx), new device-detail metrics view
- Ops: [deploy/grafana/provisioning/dashboards/](../../deploy/grafana/provisioning/dashboards/), [deploy/helm/](../../deploy/helm/) (TimescaleDB image), [loadtest](../../server/tests/loadtest/main.go)

## Verification

- **Rust:** `cargo test -p mesh-agent-core` (detector/sampler/ring, redaction, positive +
  negative); ARM bench; RSS/CPU measured.
- **Cross-language:** `make golden` for new variants; forward-compat dispatch test flipped.
- **Server:** Go handler/store tests; migrations in `testpg`; **RLS cross-tenant-deny**;
  correlation ranking (injected anomaly ranks #1); compression/CA policies applied.
- **Load (WS-8):** 500 multi-tenant agents → control-plane p99, ingest/compression,
  cardinality, correlation latency recorded; mitigation ladder applied if needed.
- **Web/E2E:** `make e2e` (badge, panel, uPlot, drill-down).
- **Manual:** `/run` stack, inject CPU/disk load + a secret-bearing process, confirm bit
  flips, correlation ranks it, cmdline is **redacted**, and RLS hides it cross-tenant.
- **Gate:** `make lint`, `make sonar`, full `/precommit`; `/refactor` before push.

## Housekeeping

- ADRs: (a) edge-first + clean-room ML, (b) multi-tenant RLS, (c) TimescaleDB adoption +
  on-existing-Postgres placement + mitigation ladder, (d) process-telemetry PII/redaction —
  index rows in [.claude/decisions.md](../decisions.md). Update [.claude/phases.md](../phases.md).
- New crates/deps + the TimescaleDB image via ADR review. `/docs` update each workstream.
