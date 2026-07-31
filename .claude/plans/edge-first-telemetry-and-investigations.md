# Edge-First Telemetry and Investigations — Master Specification

**Status:** contracts settled except §14 (two open items — stall-vitals platform
scope, and alert state in VictoriaMetrics).
**Revision:** v2, 2026-07-29. Supersedes v1 in full; §1.5 lists what v1 got wrong.

**One-line intent:** the edge becomes the only consumer of high-resolution
telemetry; central stores a fixed-cardinality vitals set plus alerts that carry
their own evidence; anomalous metrics and system events open grouped
investigations for the ops team.

**Recurring cast** (used throughout so every option is judged against something
concrete): **Contoso**, a 5 000-endpoint MSP customer. **FS01**, a file server
with a 120 GB system volume and a 2 TB data volume. **DAL-WS-012**, a Dallas
workstation. **WS-4471**, a Fabrikam workstation that freezes ~5 s every weekday
at 12:05.

| WS | Problem | Nature |
|---|---|---|
| **A** | Telemetry is silently lost; charts show ~20 min regardless of window | Defect — prerequisite |
| **B** | Central VictoriaMetrics load must stay bounded as the fleet grows | Architecture — ship policy |
| **C** | Anomalies and events must become investigable incidents | Feature |

---

## 1. Problem deconstruction

### 1.1 WS-A — what was reported, and what it actually is

Reported: "the telemetry dashboard shows only the last hour regardless of the
window chosen."

Measured: the window selector is **not** the defect. Every preset returns data
beginning at the same wall-clock instant because that is all the data that
exists. Two independent defects compound:

1. **Silent ingest loss.** The server counts a telemetry message as ingested,
   then discards it without incrementing any drop counter.
2. **Data-derived response grid.** The API returns only buckets VictoriaMetrics
   answered, so a 7 d request renders two points, indistinguishable from a 1 h
   request — the control *appears* dead.

Defect 2 is why this read as a broken selector. Fixing 1 alone leaves every real
gap looking like a broken control.

### 1.2 WS-B — what was asked, and what it actually is

Asked: "as much TSDB processing on the edge as possible, to lower VM load."

Measured (§2.2): VictoriaMetrics uses **107.9 MB of a 48.9 GB volume (0 %)** with
one device. There is no present bottleneck; this is a forward-looking design, and
it must optimise the constraint that will actually bind.

That constraint is **active series (memory)**, not sample rate. Shipping every
60 s instead of 10 s cuts VM disk ~6× and moves active series **zero**. Only
changing *which dimensions are central* moves the binding constraint.

**And there is no detail to move.** §2.4 proves the per-entity dimensions do not
exist: the agent collects one aggregated `disk_used_percent` and primary-interface
network only. The 40-series/agent figure in
[spike_test.go](../../server/tests/vmcardinality/spike_test.go) is a **projection
of a planned dimension set that was never built**.

So WS-B is honestly **preventive**: establish the O(1) boundary *before* the
expansion exists, so it can never land centrally. The win available today is the
cadence change plus a redefinition of what "central" is allowed to contain.

### 1.3 WS-C — what "investigation room" means here

Signals must become durable, groupable, assignable work items. Because central
never holds the detail behind a signal, an alert must be **self-contained**: its
evidence ships with it at fire time. Nothing can be fetched later.

**WS-C must extend the existing WS-19 alert path, not duplicate it.**
`ThresholdRule` / `AlertBreach` already exist
([control.rs:41](../../agent/crates/mesh-protocol/src/control.rs#L41)), ride
`AgentHealthSummary`, and land in VM through
[alert_breach.go](../../server/internal/agentapi/alert_breach.go). Two parallel
alerting systems would be a maintainability failure.

### 1.4 Why the three are one design

WS-B stops the detail from ever becoming central. WS-C is what makes that safe —
the detail that *matters* is promoted centrally automatically, as alert evidence,
by the detector rather than by an operator. WS-A is the prerequisite: none of it
is trustworthy on a pipeline that loses data without saying so.

### 1.5 Corrections to v1 of this plan

Recorded so the reasoning is auditable.

| v1 claim | Reality | Consequence |
|---|---|---|
| "extrema (`max`, `p99`)" | `StoredTierPoint` holds `min/max/sum/last/last_ts/count` — **no percentile structure** ([tier.rs:37](../../agent/crates/edge-tsdb/src/tier.rs#L37)) | **p99 dropped.** §2.5 shows `max` alone recovers the signal; a t-digest in the codec, merge and wire is disproportionate |
| "the agent expands to per-entity dims locally" | Those collectors **do not exist** (§2.4) | Expansion becomes a **separate later program** (§4.2); this program sets the boundary |
| "disk = worst mount (already an edge reduction)" | It is a **capacity-weighted average across all disks** (§2.4) — a live RMM defect | Redefined as worst-mount + a critical-mount count (§6.2), fixed here |
| silent on the WS-19 alert path | `ThresholdRule`/`AlertBreach` already exist | WS-C **extends** it (§6.6) |
| "~2 KB/series RAM" | A rule of thumb, cited as if measured | Marked *derived*; becomes a **measured gate** (§9.1 Q3) |
| implied Grafana as a product surface | Grafana is **ClusterIP with no ingress anywhere** — a platform-operator tool | Product views read the API; Grafana serves platform meta-monitoring only (§6.7) |

---

## 2. Empirical baseline

All figures measured against the live cluster on **2026-07-29**, method stated.
Derived figures are labelled.

### 2.1 WS-A — the loss, proven

| # | Measurement | Value | Method |
|---|---|---|---|
| M1 | Presets return data from one instant | 1h→111 pts, 6h→51, 24h→14, 7d→2, all from 03:17:20 | `query_range` at each preset's step |
| M2 | Total samples that exist | **137 at 1 h == 137 at 30 d** | `count_over_time` at five windows |
| M3 | Total samples anywhere in time | **855**, all dims, all from 03:17:20 | `/api/v1/export`, `-365 d`…`+365 d` |
| M4 | Control metric is healthy | `node_load1` → 120 / 2 880 / 20 160 at 1 h / 24 h / 7 d | same API |
| M5 | Server *received* the data | ingest counter 5 597 → 7 850 over 21:40→03:40 (~1/10 s) | scraped counter, range query |
| M6 | Zero drops, every reason | `opengate_edge_telemetry_drops_total` — **series absent** | instant query |
| M7 | Nothing deleted it | no `delete_series` in 2 299 VM log lines; no purge/reconcile in 34 780 server log lines | `kubectl logs` |
| M8 | Accumulating, not trimming | earliest pinned at 03:17:20 across four probes while latest advances | repeated export |
| M9 | VM rejected nothing | `vm_rows_ignored_total{big_timestamp}` = 0, `{small_timestamp}` = 0 | VM `/metrics` |
| M10 | Host clock jumped | `lstart` + `etime` = 20:46 vs `date` = 03:46:31 → **+7 h** | `ps`, `date` |

**M5 + M6 + M7 + M9 are the proof:** ~2 250 windows (~11 000 samples) received,
counted, never written, never deleted, never rejected, never recorded as dropped.

The only discard path in
[conn_telemetry.go](../../server/internal/agentapi/conn_telemetry.go) that
increments nothing is the `len(samples) == 0` early return in `bufferTelemetry`
(and its twin in `handleAgentHealthSummary`), reached **after** `acceptTelemetry`
increments the ingest counter.

**Grid defect:** [`assembleMetricRange`](../../server/internal/api/metrics_assemble.go)
builds `t[]` from `unionGrid(avg)` — only timestamps VM returned. M1 is the
measurement: a 1 h request whose grid should hold 360 buckets returns 111.

### 2.2 WS-B — the central store today

| Measurement | Value | Method |
|---|---|---|
| Retention, **live** | **30 d** | statefulset `-retentionPeriod` |
| Retention, **chart** | 90 d | [values.yaml](../../deploy/helm/monitoring/values.yaml) — **drift, fixed in A5** |
| Disk used | **107.9 MB / 48.9 GB (0 %)** | `df -h /storage` |
| Total active series | 4 496 | `/api/v1/status/tsdb` |
| Edge series today | 5 dims + 1 anomaly = **6/device** | `/api/v1/series` |
| Resource limits | **500 m CPU / 512 Mi RAM** | statefulset spec |
| **Cost per sample** | **0.316 B** | 107.9 MB ÷ 358 024 275 `vm_rows_added_to_storage_total` |
| Grafana exposure | **ClusterIP, no ingress cluster-wide** | `kubectl get ingress -A` |
| Cluster block-volume ceiling | 200 GB | ADR-035 |

### 2.3 WS-B — projection at the 5 000-agent target

Using the measured 0.316 B/sample. RAM uses a **derived** ~2 KB/series rule of
thumb that Q3 (§9.1) converts into a measurement.

| Shape | Series/device | Active series | RAM *(derived)* | Disk / 30 d |
|---|---|---|---|---|
| Today (6 @ 10 s) | 6 | 30 000 | ~60 MB | 2.46 GB |
| **Vitals contract (§6.2, ~19 @ 60 s)** | **19** | **95 000** | **~190 MB ✓** | **1.30 GB** |
| Unbounded trajectory (40 @ 10 s) | 40 | 200 000 | ~400 MB — **at the limit** | 16.4 GB |
| Unbounded, capped-large hosts (99 @ 10 s) | 99 | 495 000 | ~990 MB ✗ | 40.6 GB |

Read this honestly: the vitals contract **raises** series per device (6 → 19) while
**lowering** disk per device (491 → 259 KB / 30 d, −47 %). Its value is not
today's saving — it is **capping the trajectory at 24 instead of 40–99**.

### 2.4 WS-B — what the agent actually collects

| Finding | Evidence |
|---|---|
| No per-core, per-disk, per-mount or per-interface dimensions exist | `MetricSample` holds five scalars + `processes` ([sampler.rs](../../agent/crates/mesh-agent-core/src/ml/sampler.rs)) |
| Disk is a **capacity-weighted average across all disks** | `disks.iter().fold(...)` then `(total-free)/total` ([sampler.rs:197-205](../../agent/crates/mesh-agent-core/src/ml/sampler.rs#L197-L205)) |
| Network is primary-interface only | [primary_iface.rs](../../agent/crates/mesh-agent-core/src/ml/primary_iface.rs) |
| PSI is available on the reference kernel | `/proc/pressure/{cpu,memory,io}` present, both `some` and `full`; kernel 6.6.87.2 |

**The disk finding is a shipped RMM defect, not a design gap.** FS01 (120 GB
system at 98 %, 2 TB data at 10 %) reports **15.0 %**. The volume is about to fill
and **no `disk.used` threshold rule can fire** — and `disk.used` is one of only
three metrics the WS-19 rule vocabulary supports
([alert_breach.go](../../server/internal/agentapi/alert_breach.go)). Servers are
worst affected, since a small OS volume beside large data volumes is the normal
shape. Fixing it needs **no new collectors** — the code already iterates every
disk — so it is a pure O(1) reduction and lands in this program.

### 2.5 Detection arithmetic for sub-minute stalls

WS-4471 freezes ~5 s every weekday at 12:05. Over one 10-minute span:

| Sampling | Samples inside a freeze | Outcome |
|---|---|---|
| **1 Hz (today's sampler)** | ~5 | reliably observed |
| 0.1 Hz (10 s) | 0.5 | coin flip |
| 1/60 Hz | 0.083 | missed 92 % of the time |

The agent already samples fast enough. **Averaging** destroys the signal, not
sampling rate: inside a 60 s bucket at 1 Hz, a CPU-pinning freeze moves `avg` from
20 % to **26.7 %** (indistinguishable from noise) while `max` reads **100 %**.
`tier.rs` already computes min/max; the avg-only central decision discards them.

### 2.6 Edge capacity

| Measurement | Value | Source |
|---|---|---|
| Local store cap | 512 MB | `EDGE_STORE_CAP_MB` ([main.rs](../../agent/crates/mesh-agent/src/main.rs)) |
| On-disk density | ~7.3 B/sample, gate `< 12` | `edge-tsdb` gates |
| Store live on the reference host | 14.9 MB `localtsdb.redb` | filesystem |
| Retroactive reach *(derived)* | ~4 d at 1 s (T0); years at 60 s (T1) | tiering × cap ÷ density |

---

## 3. Locked architecture and hard constraints

> **The edge is the only consumer of high-resolution telemetry.
> Central receives vitals and alerts.**

| Dimension | Decision |
|---|---|
| Central content | **Vitals** (O(1) series/device) + **alerts with self-contained evidence** |
| Edge content | Detail, detection, local history, **correlation** |
| On-demand pulls | **None, anywhere** — no brokered history, no operator-triggered agent round-trips |
| Central high-res tier | **None** — no flight recorder, no hot window |
| Detection locus | Edge detects; central correlates, groups, owns lifecycle |
| Rule authorship | **Platform-curated only**; tenants tune parameters |
| Rule transport | **Declarative grammar — never shipped code** |
| Alert state in VM | **None per device**; aggregate server metrics for platform monitoring only |
| Target fleet | **5 000 agents** |
| Retention | Vitals **30 d**; alerts and investigations **1 y** |
| Investigation unit | **Incident is the room**; status lifecycle is the triage queue |

### 3.1 The vitals definition

A **vital** is a per-device signal whose **series count is O(1)** — fixed
regardless of hardware. Structural, not a judgement about importance, because
cardinality is the binding constraint.

| Class | Count scales with | Example | Central? |
|---|---|---|---|
| **Vitals** | nothing | `cpu.total` | ✅ |
| **Reductions** | nothing — the edge collapses the dimension | worst-mount disk %, count of mounts ≥ 90 % | ✅ |
| **Detail** | cores × disks × mounts × interfaces × processes | `cpu.usage{core=7}` | ❌ edge only |

**Reductions are where edge TSDB processing pays.** The FS01 defect (§2.4) is
exactly this: replacing a meaningless weighted average with two honest O(1)
reductions makes a whole class of RMM ticket detectable at zero cardinality cost.

---

## 4. Scope

### 4.1 In scope

**WS-A — telemetry integrity.** Typed drops on every discard; a zero-silent-loss
accounting invariant; request-derived response grids; untrusted-clock clamping;
retention drift reconciliation.

**WS-B — edge-first vitals.** The vitals contract and cadence change; the disk
reduction defect fix; extrema vitals; stall vitals (platform scope per §14.1); a
system-event rule pack; edge-side correlation feeding alert evidence; curated
tunable rules with retroactive re-evaluation and coverage accounting.

**WS-C — investigations.** Alert + evidence ingest and storage; incident grouping
across devices and time; status lifecycle, assignment, timeline, comments, cause
codes; triage workspace in the web client.

### 4.2 Out of scope

- **Notifications** of any kind — no push, email or Telegram.
- **Remediation actions from the room** — no restart, script, isolate, session.
- **Any on-demand pull** from an agent for analysis.
- **Central high-resolution storage** in any form.
- **Tenant-authored rule logic** (parameter overrides only).
- **Per-entity collectors** (per-core / per-disk / per-mount / per-interface) —
  a **separate later program**. This program sets the boundary they must respect.
- **eBPF**, hiccup-thread detection, synthetic probes, foreground-responsiveness
  probes — evaluated in §5 and not selected.
- Operational tooling (backup/retention scripts) — out of scope per project rules.

Undecided, not out of scope: **alert state in VictoriaMetrics** (§14.2).

### 4.3 Non-goals

- **Not a storage saving today.** §2.2 measures 0 % volume use, and §2.3 shows
  the vitals contract *raises* series count. It caps a trajectory; it does not
  reclaim space. Nobody may re-justify this as a cost optimisation.
- **Not real-time.** Vitals are 60 s. The device chart is an orientation view.
- **Not a replacement for the edge store.** Central never becomes the detail
  system of record.

---

## 5. Options considered

Recorded so they are not re-litigated. Each judged against the cast in the header.

### 5.1 Ship policy

| | Approach | Verdict |
|---|---|---|
| A | Rate reduction (60 s + deadband), cardinality unchanged | **Partially adopted.** Cuts Contoso's VM disk ~6×, but leaves active series — the binding constraint — untouched |
| B | **Cardinality reduction: fixed vitals, detail edge-only** | **Adopted.** The only option that moves the binding constraint |
| C | Read offload — central unchanged, edge answers chart reads | **Rejected.** It *is* on-demand pull; solves query latency, not VM load |
| D | Central tiered downsampling | **Rejected.** Pointless once high-res is never shipped; would own a new batch job for no ingest or cardinality relief |

### 5.2 Durability of edge-only detail

The objection: if DAL-WS-012 is wiped or stolen, its detail is gone.

**Resolved by promotion, not retention.** Detail that matters is pushed centrally
*automatically, by the detector, at the moment it becomes interesting*, as alert
evidence. Detail that never becomes interesting is discarded — which is correct.
A stolen DAL-WS-012 still has every alert and its evidence centrally, because
those were pushed when they happened. No operator action, no reachability
requirement, no erasure chase.

### 5.3 Sub-minute stall detection

| | Approach | Verdict |
|---|---|---|
| **A** | Keep extrema (`max`) beside `avg` | **Adopted.** Nearly free; §2.5 quantifies the recovered signal |
| **C** | OS-native pressure primitives (PSI) | **Adopted**, platform scope open (§14.1) |
| **D** | Kernel/system event rules over the existing log pipeline | **Adopted.** Cheap, high signal, reuses collected data |
| B | Hiccup-detector thread measuring scheduler latency | Rejected — strongest single technique, but not selected |
| E | Synthetic probes | Rejected — low coincidence probability against infrequent freezes |
| F | Foreground-responsiveness (Windows) | Rejected — invasive, platform-specific |
| G | eBPF off-CPU profiling | Rejected — previously dropped from scope |

### 5.4 Retroactive analysis ("has this happened before?")

| | Approach | Verdict |
|---|---|---|
| Central flight recorder | Push a rolling high-res blob centrally, unindexed | **Rejected.** ~5.5 GB / 48 h at 5 000 agents, a third storage path, and reaches back only 48 h |
| **Tunable detector re-run over edge history** | Push a rule; each agent evaluates it against its own store; findings return as alerts | **Adopted.** Longer reach (years at 60 s), zero central storage, no pull, and it runs on hardware you don't pay for |

### 5.5 `/correlate`

Today an **on-demand central engine** that KS-ranks which dimensions broke pattern,
reading VM ([correlate.go](../../server/internal/correlate/correlate.go)).

**Adopted: move it to the edge.** When a rule fires on FS01, the agent ranks its
own full-resolution dimensions over the event window and ships the ranking inside
the alert. The tech opens the incident and it *already says* `disk.io_wait 0.91,
backup-svc cpu 0.84` — no operator action, no VM query, and it ranks detail
central never had. Drag-to-correlate is retired.

Rejected alternatives: keeping it central on ~19 vitals at 60 s (guts it, and
remains an on-demand telemetry analysis); deferring (leaves a weakened path plus a
second migration).

### 5.6 Alert state in VictoriaMetrics — analysis only, **no option adopted**

Carried to §14.2. The findings below stand and should not be re-derived; what
does not yet exist is an option worth choosing.

- **Grafana has no ingress cluster-wide** (§2.2), so it is a platform-operator
  tool, never a product surface. Every product view — Contoso's fleet board,
  DAL-WS-012's device page — reads the OpenGate API, so a fleet-alert trend needs
  an endpoint and a chart component **regardless of which store backs it**.
  Per-device series in VM therefore buy the *product* nothing.
- **Keeping the breach series** costs O(rules × metrics) per device: at Contoso,
  up to 20 breach + 16 process-rank series per device ≈ **180 000 across the
  fleet**, driven by rule count rather than device count.
- **Rank-labelled process series are unsound as time series.** `rank=1` is a
  different process each minute, so a 24 h chart is a line whose meaning silently
  changes — a tech reads "one process pegged for an hour" when it was twelve.
- **A per-device breach count creates two answers** to "is DAL-WS-012 alerting?"
  (VM: a 60 s sampled gauge kept 30 d; Postgres: transactional, kept 1 y) — the
  silent-inconsistency class that produced WS-A.
- **A server-derived projection** removes that divergence but cannot cross-check
  its own source, losing the one case where the VM series had unique value:
  spotting alerts the storm cap silently suppressed.
- **Family anomaly rates** (5 fixed families) are genuinely O(1) and are not part
  of the contention.

---

## 6. Design

### 6.1 WS-A — telemetry integrity

**A1 — typed drops.** Every handler in `conn_telemetry.go` routes an empty sample
set to a counted drop (`empty_dims`, `empty_summary`), never a bare `return`.

**A2 — the accounting invariant (I1).** A structural test asserts, for every
telemetry message type, `ingested == persisted + Σ drops` over a driven sequence
covering every branch. A divergence alert makes regression loud. **This is the
durable fix**; A1 alone leaves the next variant just as invisible.

**A3 — request-derived grid (I4).** `assembleMetricRange` synthesises the full
grid `t[i] = from_aligned + i*step` for `i ∈ [0, span/step)` and projects VM's
answer onto it; absent buckets stay `null`. `bucket_s` already reports the step,
so no new response field. A tech selecting 7 d on FS01 sees seven days with an
honest hole, not two points.

**A4 — untrusted clock (I5).** `telemetryTimestamp` clamps to
`[now - maxBacklog, now + maxSkew]`, counts `clock_skew_clamped`, and preserves
intra-batch ordering. `maxBacklog` accommodates reconnect backfill; `maxSkew` is
minutes. Proven necessary by M10 — and laptop sleep/resume, WSL2 and VM snapshot
restore make this routine across Contoso's estate.

**A5 — retention drift.** Reconcile `values.yaml` to **30 d** and add a test
asserting the chart value equals the rendered statefulset argument.

### 6.2 WS-B1 — the vitals contract

**Cross-platform (16):**

| Vital | Note |
|---|---|
| `cpu.total`, `cpu.total.max` | max recovers WS-4471's freeze (§2.5) |
| `mem.used_percent`, `mem.used_percent.max` | |
| `disk.used_percent` | **redefined: worst mount**, fixing the FS01 defect (§2.4) |
| `disk.mounts_critical` | count of mounts ≥ 90 % — O(1) reduction over the existing iteration |
| `net.rx_bps`, `net.rx_bps.max`, `net.tx_bps`, `net.tx_bps.max` | max catches Contoso's 02:00 backup saturating the LAN |
| `node_anomaly_rate` | existing |
| `family_anomaly_rate` × 5 | existing, fixed families |

**Stall vitals (5), platform scope per §14.1:** `stall.cpu.some`,
`stall.mem.some`, `stall.mem.full`, `stall.io.some`, `stall.io.full` — from
`/proc/pressure/*` `avg60`, matching the 60 s cadence. `cpu.full` is omitted
because the kernel defines it as always zero.

**Hard cap: 24 series/device, test-enforced.** 21 used on a stall-capable host,
16 otherwise — headroom for three without re-opening the contract.

**Cadence: 60 s.** The 10 s central stream is **removed**.

**Migration.** The five existing dims keep their names; `disk.used_percent`
changes *meaning* (weighted average → worst mount), which is a deliberate
correctness fix and must be called out in the ADR and docs.

### 6.3 WS-B2 — stall detection (options A + C)

**A — extrema.** `tier.rs` already folds min/max per bucket; ship `max` beside
`avg` for the four spike-sensitive gauges. Platform-neutral. **p99 is not
included** — no percentile structure exists and `max` suffices (§1.5, §2.5).

**C — pressure.** Each stall vital is "percent of time tasks were stalled" in
`[0,100]`. PSI is ideal here: the **kernel has already performed the reduction**,
so it costs one file read and zero cardinality.

### 6.4 WS-B3 — system-event rule pack (option D)

Curated rules over the existing edge log readers, plus the second stated signal
class — **repeated errors from one service over 24 h**, a rolling per-service
error count evaluated locally. Log evidence is redacted at the edge (ADR-049);
raw lines never leave the host unredacted.

| Platform | Signal | Meaning |
|---|---|---|
| Windows | 4101 | display driver hung and recovered (TDR) |
| Windows | 129 (storport) | disk stopped responding, bus reset |
| Windows | 2004 | Windows' own Resource Exhaustion Detector |
| Linux | `hung_task` | task blocked > 120 s |
| Linux | OOM kill, ATA reset, thermal throttle | resource or hardware stall |

### 6.5 WS-B4 — edge correlation and curated tunable rules

**Edge correlation.** The KS ranking moves into `mesh-agent-core`. On fire, the
agent ranks its dimensions over the event window against a baseline window and
emits the top-N into the alert's evidence.

**Curated tunable rules.** Extends `ThresholdRule` / `PushAlertRules` /
`AlertRuleProvider`:

- **Grammar (I7):** existing comparators, sustain and hysteresis, plus
  rate-of-change, window aggregate (`max`/`mean` over N), and cross-dimension
  conjunction. Statically analysable and cost-boundable before rollout. No code,
  no WASM, no side effects.
- **Retroactive re-evaluation:** pushing a rule version schedules a bounded,
  resumable, idle-scheduled job evaluating it over local T1 history. Findings
  return as alerts marked `backfilled`. A retro scan yields **one incident per
  `(rule, scope)`**, never N live incidents, so learning a new failure mode does
  not page-flood Contoso's queue.
- **Coverage accounting (I8):** per-rule `active / unsupported / unknown` device
  counts, surfaced in the UI. Silent partial coverage is the exact failure class
  WS-A exists to eliminate.
- **Rollout safety:** canary → staged rollout; per-agent CPU/IO budget with hard
  throttle; fleet-wide alert-rate ceiling with automatic suppression; a kill
  switch effective on reconnect.

### 6.6 WS-C — alerts, evidence and incidents

**Evidence: an immutable compressed blob on the alert row in Postgres.** Zero VM
cardinality; inherits RLS tenancy and the ADR-054 erasure cascade; a frozen
snapshot is what an investigation needs. Hard size cap, validated and redacted
before insert.

Contents: the edge-correlate ranking, a bounded window of the ranked dimensions
at edge resolution, top-N processes at the event, and redacted matching log lines.

**Volume at Contoso** *(derived)*: at 0.2 alerts/device/day and ~5 KB compressed —
~1 000 alerts/day, ~1.8 GB/year at 1 y retention.

**Incident engine.** Grouping per §7.3; lifecycle
`new → acknowledged → investigating → resolved`; auto-resolve when the condition
clears and no alert arrives within `reopen_window`. An incident in `new` **is**
the triage queue — which is why no separate promotion entity exists.

### 6.7 WS-C — platform meta-monitoring

Server-exported aggregate metrics on the existing `/metrics`, already scraped:
`opengate_alerts_open`, `opengate_alerts_created_total{rule_id}`,
`opengate_alerts_suppressed_total{reason}`, `opengate_incidents_open{status}`,
`opengate_rule_coverage{rule_id,state}`. **O(rules), not O(devices).** These back
the Grafana rule that catches a bad rollout at 14:06 — platform monitoring, in
the platform tool.

This is additive and **independent of §14.2**: it is fleet-aggregate, contributes
no per-device series, and stands whatever that item resolves to.

---

## 7. Domain model

### 7.1 Entities

| Entity | Owner | Identity | Lifetime |
|---|---|---|---|
| `Vital` | agent → VM | `(org, device, name)` | 30 d |
| `Rule` | platform | `(rule_id, version)` | immutable per version |
| `RuleBinding` | server | `(org, rule_id)` | tenant parameter overrides |
| `RuleCoverage` | server | `(rule_id, version, device)` | activation state |
| `Alert` | agent → server | `(device, rule_id, rule_version, window_start)` | 1 y, immutable |
| `Evidence` | agent → server | 1:1 with `Alert` | erased with its alert |
| `Incident` | server | `(org, rule_id, scope, scope_key)` while open | 1 y |
| `IncidentEvent` | server | append-only | with incident |

### 7.2 Collaboration

```
sampler(1 Hz) ──► LocalTsdb (T0/T1/T2)      ← detail never leaves
      │                    │
      ├─► reductions ──► vitals(60 s) ──────────────► VictoriaMetrics
      │
      └─► rule evaluator ──fires──► edge correlate ──► Alert + Evidence
                                                            │
                                                            ▼
                                        AgentConn (validate, clamp, account)
                                                            │
                                                            ▼
                                   alerts store (Postgres, RLS) ──► incident engine
                                                            │              │
                                                            ▼              ▼
                                                    aggregate /metrics   API ──► web
```

### 7.3 Grouping

An `Alert` joins an open `Incident` when **all** hold: same `org_id`; same
`rule_id` (**not** `rule_version` — a rule upgrade must not fork a live incident);
scope-compatible per the rule's `group_by`; and
`now - incident.last_seen <= rule.group_window`.

| Rule shape | `group_by` | `group_window` | RMM effect |
|---|---|---|---|
| Fleet event | `org` / `group` | 30 min | Contoso's driver rollout: 312 alerts → **1** incident, 40 devices |
| Slow burn | `device` | 24 h | FS01 disk filling: re-fires fold into one incident |
| **Recurrence** | `device` | 7 d | WS-4471: 30 daily freezes → **1** incident, "30 occurrences" |

The recurrence row is load-bearing: for WS-4471 **the pattern is the diagnosis**,
and only grouping across *time* makes it visible.

Enforced by a partial unique index on `(org_id, rule_id, scope, scope_key) WHERE
status <> 'resolved'`, which makes the fold race-safe.

### 7.4 Schema (migration `008_investigations`)

`alerts` — `id, org_id, device_id → devices(id) ON DELETE CASCADE, rule_id,
rule_version, severity, metric, value, window_start, window_end, observed_at,
received_at, backfilled, incident_id, evidence bytea, evidence_codec`, with
`UNIQUE (device_id, rule_id, rule_version, window_start)` as the idempotency key.

`incidents` — `id, org_id, rule_id, scope, scope_key, severity, status,
assignee_id, opened_at, first_seen, last_seen, resolved_at, cause_code,
occurrences, device_count`.

`incident_events` — `id, org_id, incident_id → incidents(id) ON DELETE CASCADE,
at, kind, actor_id, body jsonb`, `kind ∈ {alert_folded, status_change, assignment,
comment, device_offline, resolution}`.

All three: forced RLS, `org_id`-leading indexes, mirroring the `device_inventory`
pattern.

### 7.5 Wire contract

Additive, behind a new `Alerts` capability, golden-file tested both directions;
`controlFieldCount` bumped from **83** with its per-field encoder arm.

```
Agent → Server  AgentAlert {
  alert_id, rule_id, rule_version, severity,
  metric?, value?, window_start_ts, window_end_ts, observed_ts,
  backfilled, evidence?
}
AlertEvidence { ranked[], series[], processes[], log_samples[], truncated }
```

### 7.6 Invariants

- **I1 — accounting.** Every message counted as ingested either produces
  persisted state or increments a typed drop counter. No third outcome.
- **I2 — O(1) central cardinality**, bounded by a compile-time constant.
- **I3 — self-contained alerts.** Complete at write time; no later fetch.
- **I4 — honest grids.** Request-derived, never data-derived.
- **I5 — untrusted edge input.** Timestamps, names, ids, evidence: bounded,
  validated, accounted.
- **I6 — tenancy.** Every read and write org-scoped through RLS.
- **I7 — no shipped code.** Rules are data in a bounded grammar.
- **I8 — coverage is explicit.** `active` ≠ `unsupported` ≠ `unknown`.
- **I9 — erasure.** Deleting a device erases its alerts and evidence; an incident
  surviving on other devices remains, with that device removed.

---

## 8. Components and dependencies

| Layer | Component | Change |
|---|---|---|
| Rust | `mesh-protocol` | `AgentAlert`, `AlertEvidence`, `Alerts` capability, rule-grammar extensions |
| Rust | `mesh-agent-core::ml::sampler` | disk reduction fix, extrema, `mounts_critical` |
| Rust | `mesh-agent-core::ml::pressure` **(new)** | PSI reader |
| Rust | `mesh-agent-core::alerts` | grammar extension, retroactive evaluation |
| Rust | `mesh-agent-core::correlate` **(new)** | KS ranking ported from Go |
| Rust | `mesh-agent` | event rule pack over `host_logs`, alert emit, retro job scheduling |
| Rust | `edge-tsdb` | **unchanged** (no percentile work) |
| Go | `internal/agentapi` | `conn_telemetry.go` accounting; `conn_alerts.go` **(new)** |
| Go | `internal/alerts` **(new)** | alert store + incident engine; arch-lint `mayDependOn: [dbtx]`, mirroring `inventory` |
| Go | `internal/api` | `handlers_incidents.go` **(new)**; metrics grid fix; `/correlate` retired |
| Go | `internal/correlate` | **removed** (moved to the edge) |
| Go | `internal/metrics` | aggregate alert/incident/coverage counters |
| Go | `internal/lifecycle` | erasure cascade over alerts/evidence |
| DB | `008_investigations` | three RLS tables |
| Web | `features/investigations/` **(new)** | list + detail; routes `/investigations`, `/investigations/:id` |
| Web | `features/devices/DeviceMetrics.tsx` | 60 s vitals; drag-to-correlate retired |
| Deploy | `monitoring/values.yaml` | retention 30 d; soak dashboard repointed |

---

## 9. Non-functional requirements and quality metrics

### 9.1 Performance

| # | Budget | Target |
|---|---|---|
| Q1 | Central active series | **≤ 24/device**, test-enforced |
| Q2 | Active series at 5 000 agents | ≤ 100 000 |
| Q3 | **VM RAM per active series** | **measured, not assumed** — the ~2 KB figure becomes an experiment; budget ≤ 400 MB total |
| Q4 | VM disk at 5 000 agents, 30 d | ≤ 2 GB |
| Q5 | Agent CPU, steady state | < 1 % of one core |
| Q6 | Agent CPU, retroactive job | < 5 %, hard-throttled, idle-scheduled |
| Q7 | Agent local store | ≤ 512 MB, never fills the host disk |
| Q8 | Evidence size | ≤ 64 KB compressed per alert |
| Q9 | Alert rate | per-device and per-org ceilings with storm suppression |
| Q10 | Incident list p99 | ≤ 200 ms at 10 000 open incidents |

### 9.2 Security

- **Tenancy (I6):** RLS exactly as `device_inventory`; cross-tenant regression
  tests including crafted grouping keys.
- **No shipped code (I7):** the most load-bearing constraint here. An RMM agent
  executing server-supplied code is a supply-chain weapon aimed at every customer
  estate. Bounded declarative grammar, statically analysed, cost-bounded.
- **Untrusted agent input (I5):** reject to a counted drop; never partially apply,
  never panic, never propagate NaN.
- **Log privacy:** evidence redacted at the edge (ADR-049); a test asserts no
  evidence field can carry an unredacted line, cmdline or secret.
- **Erasure (I9):** device purge cascades to alerts and evidence; verified in the
  existing lifecycle rehearsal.
- Runs through the pen-test gate (ADR-027) with the rest of the diff.

### 9.3 Maintainability

1. **Cardinality is a test**, not a convention (Q1).
2. **One accounting rule (I1)** across every ingest path.
3. **Rules are content, not code** — detection evolves without a fleet upgrade.
4. **Coverage is never implied** (I8).
5. **Additive protocol** — new capability, golden files, encoder count bumped.
6. **One alerting system** — WS-C extends WS-19 rather than paralleling it.

### 9.4 Quality gates

Project standard applies unchanged: `make lint`, `make test`, `make golden`,
`make e2e`, coverage ≥ 80 %, mutation floors (Rust/Go/web ≥ 85 %), `make dead-code`,
`make taint-go` / `taint-web`, `make shell-quality`, SonarCloud quality gate with
**no suppressions**, PMAT TDG ≥ B+ on touched files, `go-arch-lint` clean.

---

## 10. Edge and error cases

| # | Case | Required behaviour |
|---|---|---|
| E1 | Metric message with empty payload | Typed drop, counted. **Never a silent return** |
| E2 | Agent clock skewed (**observed +7 h**) | Clamp, count `clock_skew_clamped`, preserve intra-batch order |
| E3 | Window with no data | Full request-derived grid of `null`s |
| E4 | Device offline mid-window | `null` across the gap; **never interpolate** |
| E5 | Device in maintenance | Sampling suppressed; no new alerts; an incident open *before* maintenance does **not** auto-resolve |
| E6 | Agent predates a vital or rule | Vital absent; rule counted `unsupported` — visible, never silent |
| E7 | Duplicate alert after reconnect | Idempotent on the §7.4 unique key |
| E8 | Retroactive findings | `backfilled`; folded by real event time; **one incident per (rule, scope)** |
| E9 | Alert storm across Contoso | Per-device and per-org ceilings; excess folded into one storm incident **with a count**; never silently dropped |
| E10 | Bad rule degrades customer machines | Per-agent budget + throttle; canary; kill switch on reconnect |
| E11 | Evidence exceeds the cap | Truncated at the edge with `truncated: true`; never rejected wholesale |
| E12 | Evidence contains a secret or raw log line | Redacted at the edge; asserted by test |
| E13 | DAL-WS-012 purged mid-incident | Its alerts and evidence erased; incident survives minus that device; empty incident closed |
| E14 | Org purged | Full cascade; no orphan incidents |
| E15 | Two rules fire on one condition | Distinct incidents unless `group_by` says otherwise |
| E16 | Rule upgraded while an incident is open | Grouping keys on `rule_id`; the live incident does not fork |
| E17 | Fleet event where no host individually breaches | Low-severity **observations** fold into an incident only on cross-device co-occurrence |
| E18 | VM unavailable | Vitals reads degrade to 503; **alerts and incidents keep working** |
| E19 | Postgres unavailable | Alert buffered at the edge, retried on reconnect; never acknowledged as stored when it is not |
| E20 | Agent local store corrupt | Recreate rather than crash; coverage reports `unknown` until rebuilt |
| E21 | Host disk nearly full | Free-space backoff shrinks the cap; retroactive jobs suspend first |
| E22 | Newly enrolled device | Rules activate immediately; retroactive scope empty — reported honestly |
| E23 | **FS01-shaped multi-volume host** | Worst-mount reduction; `disk.mounts_critical` ≥ 1 makes the 98 % volume visible |
| E24 | Agent offline for days with pending alerts | Bounded local queue, oldest-dropped-with-count, replayed idempotently on reconnect |
| E25 | Sudden death, no precursor | **Accepted loss** (§11) |

---

## 11. Accepted losses

1. **Sudden death with no telemetry precursor** is not investigable
   post-mortem. Central high-resolution data would not have helped — such an
   event has no signature. Gradual exhaustion alerts *before* death instead.
2. **Retroactive reach is bounded** by the local store and device survival.
3. **Sub-60 s resolution is unavailable centrally.** Extrema preserve a short
   event's existence and magnitude, not its shape.
4. **Cross-device correlation of sub-threshold signals** depends on observation
   rules authored in advance (E17).
5. **Drag-to-correlate is retired**; ranking arrives with the alert instead.

---

## 12. Acceptance criteria

**WS-A**
- A1 An `AgentMetricWindow` with empty `Dims` increments a typed drop; no code
  path discards a counted message without accounting *(I1, driven per branch)*.
- A2 `ingested == persisted + Σ drops` holds across a driven sequence.
- A3 A 7 d request over a device with 20 min of data returns the full 7 d grid
  with `null`s; point count equals `span/step`.
- A4 A sample 7 h in the past or future is clamped and counted; ordering preserved.
- A5 Chart retention equals the rendered statefulset argument.

**WS-B**
- B1 A device contributes ≤ 24 central series; the test fails on a 25th.
- B2 At Contoso scale the vitals set measures ≤ 100 000 active series and
  ≤ 2 GB / 30 d.
- B3 **FS01 fixture** (120 GB @ 98 %, 2 TB @ 10 %) reports `disk.used_percent`
  ≈ 98 and `disk.mounts_critical` = 1 — the current code returns ≈ 15 and 0.
- B4 A 5 s CPU-pinning stall inside a 60 s bucket yields `cpu.total.max` ≈ 100
  while `avg` stays < 30.
- B5 Stall vitals equal the kernel's `avg60` within tolerance.
- B6 Each event rule fires exactly once per matching event and never on a
  non-matching one.
- B7 Edge correlation ranks a synthetic broken-pattern dimension first, matching
  the Go engine's ranking on the same fixture *(port-equivalence)*.
- B8 A pushed rule reports `active + unsupported + unknown == fleet size`.
- B9 A retroactive scan over local history produces the expected alerts, marked
  `backfilled`, in **one** incident per `(rule, scope)`.
- B10 Retroactive job stays within Q6 and suspends under disk pressure.

**WS-C**
- C1 An alert is stored with evidence in one transaction; a duplicate is a no-op.
- C2 **Contoso rollout fixture**: 312 alerts across 40 devices in 29 min → exactly
   **1** incident, `device_count = 40`, `occurrences = 312`.
- C3 **WS-4471 fixture**: 30 daily alerts over 30 d → **1** incident with
   `occurrences = 30`.
- C4 **FS01 fixture**: re-fires over 6 days fold into one incident.
- C5 Status transitions are validated; invalid transitions rejected.
- C6 Auto-resolve fires only after `reopen_window` with no alert; a maintained
   device's open incident does not auto-resolve (E5).
- C7 Cross-tenant read denied, including via a crafted grouping key.
- C8 Purging DAL-WS-012 erases its alerts and evidence, leaves the incident with
   `device_count = 39` (E13).
- C9 Evidence over the cap is truncated with `truncated: true`.
- C10 No evidence field can carry an unredacted log line or cmdline.
- C11 Aggregate `/metrics` counters are O(rules): fleet size does not change
   series count.
- C12 Incident list p99 ≤ 200 ms at 10 000 open incidents.
- C13 Golden round-trip for the new control pair passes both directions.

---

## 13. Implementation steps

TDD order throughout: every step writes its failing test first.

**WS-A (ships alone, first)**
1. Typed drops + accounting invariant test *(A1, A2)*.
2. Request-derived grid + client grid-change tolerance *(A3)*.
3. Timestamp clamping + counter *(A4)*.
4. Retention reconciliation + drift test *(A5)*.

**WS-B**
5. Disk reduction fix + `mounts_critical`, FS01 fixture *(B3)*.
6. Extrema vitals *(B4)*; vitals contract + ≤ 24 cap test *(B1)*; cadence change;
   10 s stream removal; measured budgets *(B2, Q3)*.
7. PSI reader + stall vitals *(B5)*.
8. Event rule pack + per-service 24 h error rule *(B6)*.
9. Port KS ranking to `mesh-agent-core`, prove port-equivalence, retire
   `internal/correlate` and the drag UX *(B7)*.
10. Rule grammar extension + coverage accounting *(B8)*.
11. Retroactive evaluation job + budget/suspend behaviour *(B9, B10)*.
12. Rollout safety: canary, throttle, ceiling, kill switch.

**WS-C**
13. Wire contract + capability + goldens *(C13)*.
14. Migration `008`, RLS store, ingest with I1 accounting *(C1, C7)*.
15. Incident engine: grouping, lifecycle, auto-resolve *(C2–C6)*.
16. Erasure cascade *(C8)*; evidence caps and redaction *(C9, C10)*.
17. Aggregate `/metrics` *(C11)*. Soak-dashboard changes wait on §14.2.
18. API + OpenAPI + Go/TS regeneration *(C12)*.
19. Web investigations feature; device page incidents strip; chart on vitals.

**Close-out**
20. Budget measurements captured as evidence; E2E; docs; ADRs +
    `decisions.md` rows; `phases.md`; archive this plan.

---

## 14. Open items

### 14.0 How these interact with the rest of the plan

Neither open item blocks WS-A, nor steps 5–11 of WS-B. §14.2 must resolve before
WS-C step 17 (aggregate metrics + dashboard repoint) and before Q1's enforcement
scope can be finalised.

### 14.1 Stall-vitals platform scope — **OPEN**

PSI is Linux-only. Options: Windows parity required before shipping stall vitals;
Linux first with Windows following under explicit coverage accounting; or Linux
only, treating continuous stall measurement as a server-side capability.

**Consequence to weigh:** the case that motivated stall detection is a
*workstation* freeze (WS-4471), and Windows is usually the majority of an RMM
fleet. Note that extrema (A) are platform-neutral and the event pack (D) is
largely Windows-specific, so Windows is not left empty under any option — it
gets `max`-based spike visibility and event-driven stall evidence, just not a
continuous "percent of time stalled" gauge.

### 14.2 Alert state in VictoriaMetrics — **OPEN, re-evaluate later**

The three existing edge series that are "detail" by the O(1) rule —
`opengate_edge_alert_breach{rule,metric}` and
`opengate_edge_process_{cpu,mem}_percent{rank}` — have **no satisfying
resolution**. Three shapes were analysed (§5.6) and none was adopted: keeping
them breaches the O(1) rule and charts a semantically-shifting line; an
agent-reported count creates two disagreeing answers; a server-derived projection
is consistent but cannot cross-check its own source; removing them entirely costs
the only external observation path for a bad rollout. The analysis in §5.6 stands
and should not be re-derived.

**Interim position:** this program **does not change these series**. The soak
dashboard keeps working and nothing regresses.

**Revisit when** two things are known that are currently only *derived*: the real
fleet-board query shape (once the incident engine and its API exist), and
measured alert volume at Contoso scale rather than the 0.2/device/day estimate in
§6.6. Both make the trade concrete instead of hypothetical.

**Known tension to resolve with it:** Q1 caps central series at 24/device. If
breach series persist, that cap is breached whenever many rules fire on one
device, so **Q1's enforcement scope (vitals only, or all edge series) is defined
by whatever §14.2 resolves to.** Until then Q1 is enforced over the vitals set.

Everything else in this document is settled.

---

## 15. Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Removing the 10 s stream reads as a regression | High | Medium | Deliberate; extrema preserve event visibility; docs state current behaviour |
| A bad curated rule degrades 5 000 machines | Low | **High** | Declarative grammar, static cost analysis, canary, throttle, kill switch |
| Alert storm floods Postgres | Medium | High | Per-device/per-org ceilings, storm folding with counts, evidence cap |
| Grouping merges unrelated events | Medium | Medium | Keyed on `rule_id` + scope + window; E15 |
| Grouping fragments one event | Medium | Medium | Per-rule `group_window`; C2/C3 fixtures |
| Silent partial rule coverage | Medium | High | I8 — the WS-A lesson applied forward |
| Retroactive job harms a customer machine | Low | High | Idle-scheduled, throttled, suspends first |
| Vitals set creeps past O(1) | Medium | High | Q1 cap test |
| Evidence leaks PII or secrets | Low | High | Edge redaction + test + pen-test gate |
| Disk-semantics change surprises existing rules | Medium | Medium | Called out in ADR + docs; B3 fixture pins both values |
| Edge correlate diverges from the Go engine | Medium | Low | B7 port-equivalence test on shared fixtures |
| Scope creep into notifications/remediation | High | Medium | §4.2 forbids both; ADR records the deferral |

---

## 16. Decisions

| # | Decision | Where |
|---|---|---|
| D1 | **Edge is the only consumer of high-resolution telemetry** | §3 |
| D2 | **No on-demand pulls anywhere**; no central high-res tier | §3, §4.2 |
| D3 | **Vital = O(1) series per device**; cap **24**, test-enforced | §3.1, §6.2 |
| D4 | Vitals cadence **60 s**; the 10 s central stream is removed | §6.2 |
| D5 | `disk.used_percent` redefined to **worst mount** + `mounts_critical` — fixes a live RMM defect | §2.4, §6.2 |
| D6 | Stall detection = **extrema + OS pressure + event rules**; hiccup thread, probes and eBPF rejected | §5.3 |
| D7 | **p99 dropped** — no percentile structure exists and `max` suffices | §1.5 |
| D8 | **Zero-silent-loss accounting invariant (I1)** on every ingest path | §6.1 |
| D9 | Metric grids are **request-derived** | §6.1 |
| D10 | Agent timestamps are **untrusted and clamped** | §6.1 |
| D11 | Rules are **platform-curated declarative data — never shipped code** | §3, §9.2 |
| D12 | New detectors run **retroactively over local history**; one incident per (rule, scope) | §6.5 |
| D13 | Rule **coverage explicit per device** | §6.5 |
| D14 | **`/correlate` moves to the edge**; ranking ships inside the alert; drag UX retired | §5.5 |
| D15 | **Per-entity collectors deferred to a separate program**; this one sets the boundary | §4.2 |
| D16 | **Aggregate O(rules) server metrics** for platform meta-monitoring. Whether any **per-device** alert series belongs in VM is **OPEN** | §6.7, §14.2 |
| D17 | Evidence is an **immutable compressed blob on the alert row in Postgres** | §6.6 |
| D18 | **Incident is the room**; status lifecycle is the triage queue | §7 |
| D19 | Grouping is **two-axis** — declared scope × declared window; recurrence across time required | §7.3 |
| D20 | Target **5 000 agents**; vitals 30 d; alerts and investigations 1 y | §3 |
| D21 | **No notifications, no remediation actions** in this program | §4.2 |
| D22 | WS-C **extends** the WS-19 alert path; there is one alerting system | §1.3, §9.3 |
