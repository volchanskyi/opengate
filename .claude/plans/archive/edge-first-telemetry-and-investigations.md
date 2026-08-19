# Edge-First Telemetry and Investigations — Master Specification

**Status:** all contracts settled. No open items — §14 records two *deferrals*,
each with a named revisit trigger, which are decisions rather than questions.
**Revision:** v3, 2026-08-04. Supersedes v2; §1.5 lists what v1 got wrong and
§1.6 records what v3 settled.

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
[spike_test.go](../../../server/tests/vmcardinality/spike_test.go) is a **projection
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
([control.rs:41](../../../agent/crates/mesh-protocol/src/control.rs#L41)), ride
`AgentHealthSummary`, and land in VM through
[alert_breach.go](../../../server/internal/agentapi/alert_breach.go). Two parallel
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
| "extrema (`max`, `p99`)" | `StoredTierPoint` holds `min/max/sum/last/last_ts/count` — **no percentile structure** ([tier.rs:37](../../../agent/crates/edge-tsdb/src/tier.rs#L37)) | **p99 dropped.** §2.5 shows `max` alone recovers the signal; a t-digest in the codec, merge and wire is disproportionate |
| "the agent expands to per-entity dims locally" | Those collectors **do not exist** (§2.4) | Expansion becomes a **separate later program** (§4.2); this program sets the boundary |
| "disk = worst mount (already an edge reduction)" | It is a **capacity-weighted average across all disks** (§2.4) — a live RMM defect | Redefined as worst-mount + a critical-mount count (§6.2), fixed here |
| silent on the WS-19 alert path | `ThresholdRule`/`AlertBreach` already exist | WS-C **extends** it (§6.6) |
| "~2 KB/series RAM" | A rule of thumb, cited as if measured | Marked *derived*; becomes a **measured gate** (§9.1 Q3) |
| implied Grafana as a product surface | Grafana is **ClusterIP with no ingress anywhere** — a platform-operator tool | Product views read the API; Grafana serves platform meta-monitoring only (§6.7) |

### 1.6 What v3 settled

v2 left two items open and a further nine parameters stated only as prose
("minutes", "per-device and per-organization ceilings", "top-N"). A parameter without a
value is not a decision, so each is now fixed.

| Item | v2 state | v3 |
|---|---|---|
| Stall-vitals platform scope | open | **Linux-only**, no analogue invented for any other platform (§6.3, D23) |
| Alert state in VM | open | **Deferred with a named trigger** — this program changes nothing (§14.2, D16) |
| Rule storage and distribution | unstated | **Embedded catalogue + DB binding/rollout state**, selector-keyed (§6.5, D24) |
| 1 y retention enforcement | declared, no mechanism | **Policy declared, enforcement deferred**; tech-debt row (§14.3, D25) |
| `maxBacklog` / `maxSkew` | "minutes" | **7 d / 5 min** (§6.1 A4) |
| `reopen_window` | unstated | **defaults to the rule's `group_window`** (§6.6) |
| Alert-rate ceilings | "ceilings" | **20/device/h, 500/organization/h** (§6.6) |
| Evidence composition and codec | "top-N", unstated codec | **fixed shape, DEFLATE** (§6.6) |
| Severity and cause-code vocabularies | unstated | **closed sets** (§6.6) |
| Canary and rollout stages | "canary → staged" | **5 devices → 10 % → 100 %**, gated (§6.5) |
| Rule metric vocabulary | mismatched with vitals names | **aligned, legacy names aliased** (§6.5) |
| `controlFieldCount` | 83 | **86** — v2's figure was already stale (§7.5) |
| Q2 active-series budget | 100 000, inconsistent with Q1×fleet | **derived as Q1 × fleet = 120 000** (§9.1) |
| VM pod sizing | implied by an unmeasured ~2 KB/series | **Sized after Q3 measures**; owner decides between four stated options (§9.1, D32) |
| Retroactive reach | "years at 60 s", derived as cap ÷ density | **Measured: ~7 months** at the shipped cap and today's vitals shape (§9.1.1, D34) |
| Alert rate | 0.2/device/day, estimated | **Q12 measures it** before the pack leaves canary (§9.1.2, D35) |
| Disk performance | **absent entirely** — only capacity was collected | `await_ms` + `.max` + `queue_depth`, worst device, Linux only; `%util` excluded for an SSD/NVMe fleet (§6.3.1, D37) |
| VMs and containers | unaddressed | Both in scope; container switches to cgroup source and reports `unsupported` for latency (§6.3.2, D38) |

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
[conn_telemetry.go](../../../server/internal/agentapi/conn_telemetry.go) that
increments nothing is the `len(samples) == 0` early return in `bufferTelemetry`
(and its twin in `handleAgentHealthSummary`), reached **after** `acceptTelemetry`
increments the ingest counter.

**Grid defect:** [`assembleMetricRange`](../../../server/internal/api/metrics_assemble.go)
builds `t[]` from `unionGrid(avg)` — only timestamps VM returned. M1 is the
measurement: a 1 h request whose grid should hold 360 buckets returns 111.

### 2.2 WS-B — the central store today

| Measurement | Value | Method |
|---|---|---|
| Retention, **live** | **30 d** | statefulset `-retentionPeriod` |
| Retention, **chart** | 90 d | [values.yaml](../../../deploy/helm/monitoring/values.yaml) — **drift, fixed in A5** |
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
| **Vitals contract (§6.2, 24 @ 60 s)** | **24** | **120 000** | **~240 MB ✓** | **1.64 GB** |
| Unbounded trajectory (40 @ 10 s) | 40 | 200 000 | ~400 MB — **at the limit** | 16.4 GB |
| Unbounded, capped-large hosts (99 @ 10 s) | 99 | 495 000 | ~990 MB ✗ | 40.6 GB |

**24 is the per-device count** — the agent implements Linux, and stall vitals and
disk-performance vitals both ship there (§6.3), so every device in the fleet
contributes 24. That makes Q2 exact rather than approximate, since Q2 is
*derived* as Q1 × fleet. A platform whose agent cannot supply the eight
Linux-only vitals would contribute 16; sizing stays at 24 fleet-wide, because a
capacity plan must not assume the cheaper platform mix.

Read this honestly: the vitals contract **raises** series per device (6 → 24) while
**lowering** disk per device (491 → 328 KB / 30 d, −33 %). Its value is not
today's saving — it is **capping the trajectory at 24 instead of 40–99**.

### 2.4 WS-B — what the agent actually collects

| Finding | Evidence |
|---|---|
| No per-core, per-disk, per-mount or per-interface dimensions exist | `MetricSample` holds five scalars + `processes` ([sampler.rs](../../../agent/crates/mesh-agent-core/src/ml/sampler.rs)) |
| Disk is a **capacity-weighted average across all disks** | `disks.iter().fold(...)` then `(total-free)/total` ([sampler.rs:197-205](../../../agent/crates/mesh-agent-core/src/ml/sampler.rs#L197-L205)) |
| Network is primary-interface only | [primary_iface.rs](../../../agent/crates/mesh-agent-core/src/ml/primary_iface.rs) |
| PSI is available on the reference kernel | `/proc/pressure/{cpu,memory,io}` present, both `some` and `full`; kernel 6.6.87.2 |
| **No disk *performance* signal exists at all** | `MetricSample` has `disk_used_percent` and nothing else about disks ([sampler.rs](../../../agent/crates/mesh-agent-core/src/ml/sampler.rs)); `sysinfo 0.39`'s `Disk::usage()` would give bytes but no service time or queue depth |
| `/proc/diskstats` carries what is needed | Live on the reference host: per-device completed I/Os, ms spent reading and writing, in-flight count, and weighted ms — the inputs `iostat` derives `await` and `avgqu-sz` from |
| `/sys/block/` is the non-partition device list | Present; partitions do not appear, so it is the natural filter against double-counting `nvme0n1` and `nvme0n1p1` |
| cgroup v2 is live, with `io.stat` and `io.pressure` | Both present at the cgroup root; `stat -fc %T /sys/fs/cgroup` = `cgroup2fs` |

**The disk finding is a shipped RMM defect, not a design gap.** FS01 (120 GB
system at 98 %, 2 TB data at 10 %) reports **15.0 %**. The volume is about to fill
and **no `disk.used` threshold rule can fire** — and `disk.used` is one of only
three metrics the WS-19 rule vocabulary supports
([alert_breach.go](../../../server/internal/agentapi/alert_breach.go)). Servers are
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
| Local store cap | 512 MB | `EDGE_STORE_CAP_MB` ([main.rs](../../../agent/crates/mesh-agent/src/main.rs)) |
| On-disk density | ~7.3 B/sample, gate `< 12` | `edge-tsdb` gates |
| Store live on the reference host | 14.9 MB `localtsdb.redb` | filesystem |
| Retroactive reach *(measured, Q11)* | ~7 months at 60 s (T1), at ~2.3 B per stored reading | [reach_test.rs](../../../agent/crates/mesh-agent-core/tests/reach_test.rs) |

Both rows are **measured** figures. The reach row is measured at steady state
through the production write path at three cap sizes, and holds only for the
vitals shape beside it: a device storing more series reaches proportionally less
far (§9.1.1).

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
| Rule storage | **Definitions embedded in the server** (versioned, CI-analysed); **bindings and rollout state in Postgres** |
| Alert state in VM | **New** per-device alert series: none. Aggregate O(rules) server metrics added. The three existing WS-19 edge series are **unchanged by this program** (§14.2) |
| Target fleet | **5 000 agents** |
| Retention | Vitals **30 d**; alerts and investigations **1 y declared**, enforcement deferred (§14.3) |
| Investigation unit | **Incident is the room**; status lifecycle is the triage queue |
| Tenancy levels | **tenant → organization → site → device** (§3.2). Isolation at the **tenant**; ceilings, grouping and rule settings key on the **organization** |

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

### 3.2 Tenancy levels

Settled by the tenancy rework, which lands before any of this. Four levels:

| Level | Means | Role here |
|---|---|---|
| **Tenant** | the MSP | The wall the database enforces. Every table in §7.4 carries `tenant_id` and is isolated on it |
| **Organization** | one customer (Contoso) | **The scoping level for this programme** — the hourly alert ceiling, the broadest incident grouping scope, and rule-settings resolution all key here |
| **Site** | a location or department inside a customer | A narrower rule-settings level, and a narrower grouping scope |
| **Device** | one machine | The narrowest level of both |

Why this matters to the design rather than being an administrative detail: the ceiling and the
grouping scope must sit at the **organization**. At the tenant they would let Contoso's storm
consume Fabrikam's alert budget, and would fold Contoso's driver rollout and Fabrikam's unrelated
outage into a single incident — the exact "one name, two meanings" failure §2.4 exists to fix,
applied to tenancy.

Every table therefore carries **both** `tenant_id` (isolation) and `organization_id` (scoping).
They are different columns answering different questions and neither substitutes for the other.

---

## 4. Scope

### 4.1 In scope

**WS-A — telemetry integrity.** Typed drops on every discard; a zero-silent-loss
accounting invariant; request-derived response grids; untrusted-clock clamping;
retention drift reconciliation.

**WS-B — edge-first vitals.** The vitals contract and cadence change; the disk
reduction defect fix; extrema vitals; stall vitals (Linux only, §6.3); a
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
- **Age-based deletion of alerts, evidence and incidents.** The 1 y policy is
  declared; the sweep that enforces it is a later program (§14.3).
- **Changing the three existing WS-19 edge series** (§14.2). They keep working
  exactly as they do today.
- **A rule-authoring surface.** Platform staff enable, disable and tune curated
  rules; nobody composes new predicates at runtime (§6.5).

Nothing in this document is undecided.

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
| **C** | OS-native pressure primitives (PSI) | **Adopted, Linux only** — no analogue invented for any other platform (D23) |
| **D** | Kernel/system event rules over the existing log pipeline | **Adopted.** Cheap, high signal, reuses collected data |
| B | Hiccup-detector thread measuring scheduler latency | Rejected — strongest single technique, but not selected |
| E | Synthetic probes | Rejected — low coincidence probability against infrequent freezes |
| F | Foreground-responsiveness (per-platform desktop probes) | Rejected — invasive, platform-specific |
| G | eBPF off-CPU profiling | Rejected — previously dropped from scope |

### 5.4 Retroactive analysis ("has this happened before?")

| | Approach | Verdict |
|---|---|---|
| Central flight recorder | Push a rolling high-res blob centrally, unindexed | **Rejected.** ~5.5 GB / 48 h at 5 000 agents, a third storage path, and reaches back only 48 h |
| **Tunable detector re-run over edge history** | Push a rule; each agent evaluates it against its own store; findings return as alerts | **Adopted.** Longer reach (**measured ~7 months** at 60 s, §9.1.1), zero central storage, no pull, and it runs on hardware you don't pay for |

### 5.5 `/correlate`

Today an **on-demand central engine** that KS-ranks which dimensions broke pattern,
reading VM.

**Adopted: move it to the edge.** When a rule fires on FS01, the agent ranks its
own full-resolution dimensions over the event window and ships the ranking inside
the alert. The tech opens the incident and it *already says* `disk.io_wait 0.91,
backup-svc cpu 0.84` — no operator action, no VM query, and it ranks detail
central never had. Drag-to-correlate is retired.

Rejected alternatives: keeping it central on 21 vitals at 60 s (guts it, and
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
intra-batch ordering. Proven necessary by M10 — and laptop sleep/resume, WSL2 and
VM snapshot restore make this routine across Contoso's estate.

| Bound | Value | Why this value |
|---|---|---|
| `maxBacklog` | **7 d** | Must cover the longest reconnect backfill a bounded local queue can present (E24). Sits well inside VM's 30 d retention, so a clamped-but-legitimate backfill sample still lands in a queryable window. |
| `maxSkew` | **5 min** | Larger than any NTP-correctable drift, smaller than one vitals bucket ×5. The observed +7 h (M10) clamps hard, which is the intent. |

Both are named constants with the reasoning above in a doc comment, not literals.

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

**Disk-performance vitals (3), Linux only:** `disk.await_ms`,
`disk.await_ms.max`, `disk.queue_depth` — from `/proc/diskstats`, reduced to the
worst device. Capacity and performance are **different questions on different
entities**: `disk.used_percent` is per *mount*, these are per *physical device*.
Design in §6.3.1.

**Stall vitals (5), Linux only:** `stall.cpu.some`, `stall.mem.some`,
`stall.mem.full`, `stall.io.some`, `stall.io.full` — from `/proc/pressure/*`
`avg60`, matching the 60 s cadence. `cpu.full` is omitted because the kernel
defines it as always zero. On a host without PSI these vitals are **absent, and
the rules that watch them report `unsupported`** (I8, E6) — never zero, never a
substitute reading.

**Hard cap: 24 series/device, test-enforced.** All 24 are used, since the agent
implements Linux. **The reserved headroom is now spent** — the next vital of any
kind re-opens D3, which is the intended friction rather than an oversight.

**Cadence: 60 s.** The 10 s central stream is **removed**.

**Migration.** The five existing dims keep their names; `disk.used_percent`
changes *meaning* (weighted average → worst mount), which is a deliberate
correctness fix and must be called out in the ADR and docs.

### 6.3 WS-B2 — stall detection (options A + C)

**A — extrema.** `tier.rs` already folds min/max per bucket; ship `max` beside
`avg` for the four spike-sensitive gauges. Platform-neutral. **p99 is not
included** — no percentile structure exists and `max` suffices (§1.5, §2.5).

**C — pressure. Linux only, by decision.** Each stall vital is "percent of time
tasks were stalled" in `[0,100]`. PSI is ideal here: the **kernel has already
performed the reduction**, so it costs one file read and zero cardinality.

The rule that makes this Linux-only is about naming, not about Linux: a platform
without a time-in-stall primitive must not have one synthesized from counters
that measure different things in different units, because publishing those under
a `stall.*` name repeats precisely the FS01 mistake this program exists to fix —
one name, two meanings, silently. A platform without PSI is not left blind: it
keeps `cpu.total.max` (which B4 proves catches WS-4471's 5 s freeze, `max` ≈ 100
while `avg` < 30) and the worst-mount disk reductions, and what it lacks is only
the *continuous* "percent of time stalled" gauge. Coverage accounting reports
that gap as `unsupported` (I8, E6) rather than implying it away.

**PSI is read from the agent's own cgroup when containerized** (§6.3.2), so a
containerized agent measures its own pressure rather than the host's.

*Extension point:* a Windows agent would add a `/proc/pressure`-equivalent
collector only if Windows grows a genuine time-in-stall primitive; until then it
supplies the 16 platform-neutral vitals, its own event rules over an Event Log
reader, and `unsupported` for `stall.*` and the disk-performance trio.

### 6.3.1 Disk performance — SSD/NVMe targeted, Linux only

The fleet is **purely SSD/NVMe**, and that decides the metric set. Today no disk
performance signal exists at all (§2.4): `disk.used_percent` answers "is it
full", nothing answers "is it slow".

**`disk.busy_percent` (`%util`) is deliberately NOT a vital.** It is the obvious
CPU-utilization analogue and it is the wrong metric here: SSD and NVMe service
many I/Os in parallel, so `%util` pins at 100 % while the device still has
substantial headroom. On a purely SSD/NVMe fleet it would produce confident,
constant, meaningless saturation. Recording the omission so nobody adds it later
for symmetry with `cpu.total`.

| Vital | Derivation from `/proc/diskstats` | What it catches |
|---|---|---|
| `disk.await_ms` | `Δ(ms_read + ms_write) / Δ(reads + writes)` | Average service time per I/O — **the** SSD/NVMe health signal. A wearing, thermally-throttled or GC-stalling device shows here while capacity and throughput look normal |
| `disk.await_ms.max` | max over the bucket's 1 Hz samples | A 5 s I/O stall barely moves a 60 s average but pins `max` — the same arithmetic as §2.5/B4, applied to disk |
| `disk.queue_depth` | `Δ(weighted_ms) / Δwall_ms` (`iostat`'s `avgqu-sz`) | Honest saturation on parallel devices: it keeps scaling where `%util` saturates, so "busy but healthy" is distinguishable from "overloaded" |

**Device selection: worst device, per metric, independently.** Highest `await_ms`
and highest `queue_depth` may name different devices — each answers its own
question, and per-device detail rides the alert evidence (§6.6) when a rule
fires. This is the §2.4 worst-mount fix applied on the right axis: a mean across
an idle data disk and a struggling system disk describes neither.

**Device filter:** members of `/sys/block/` (which excludes partitions by
construction), minus `loop*`, `ram*` and `zram*`. `dm-*` and `md*` are
**included deliberately** — LUKS and RAID overhead is latency the user actually
experiences, and worst-of selection does not double-count the way summing would.

**What this makes visible, in RMM terms:**

- **DAL-WS-012's NVMe wearing out.** `await_ms` drifts 2 ms → 40 ms over two
  weeks while capacity is flat and CPU is idle. Today this is invisible: the
  tech gets "it's slow" tickets and eventually reimages a healthy OS. As a slow
  burn rule (`group_by: device`, 24 h window) it opens one incident days before
  the user complains.
- **FS01's 02:00 backup.** `queue_depth` hits 28 on the data device while
  `await_ms` stays at 3 ms — saturated but healthy. `%util` would read 100 % and
  say nothing.
- **WS-4471's 12:05 freeze.** `cpu.total.max` already says 100 %. If
  `disk.await_ms.max` also spikes to 800 ms, the freeze is I/O-bound, not
  CPU-bound — a completely different fix. This is what makes the extrema pair
  actionable rather than merely alarming.

### 6.3.2 Virtual machines and containers

Both are in scope, and they need different handling. Each host resolves to
**exactly one** source, reported through coverage accounting (I8) so the shape of
what is being measured is never implied.

| Environment | Source | Vitals available |
|---|---|---|
| Bare metal, **VM** | `/proc/diskstats` | All three |
| **Container** (agent inside a cgroup) | `/sys/fs/cgroup/…/io.stat` + `io.pressure` | `stall.io.*` only; `disk.await_ms` / `queue_depth` report `unsupported` |
| No `/proc/diskstats`, no cgroup v2 | — | `unsupported` |

**Virtual machines need no special handling, and guest-observed latency is the
signal you want.** `/proc/diskstats` inside a guest reports the virtual device
(`vda`, `xvda`, `nvme0n1`), and the latency it measures **includes host
contention and volume throttling** — which is precisely what makes the
customer's application slow. The device filter must therefore accept `vd*`,
`xvd*`, `nvme*` and `sd*` alike, which `/sys/block/` membership does for free.

This yields a diagnosis nothing else in the system can reach: **a cloud volume
hitting its provisioned IOPS cap** shows `await_ms` climbing while `queue_depth`
rises and bytes/sec flatlines against a ceiling. Capacity monitoring sees a
half-empty disk; the customer sees a stalled application. OpenGate's own OCI
block volumes (ADR-035) have exactly this shape.

**Containers are the trap, and the honest answer is a reduced set.**
`/proc/diskstats` is **not namespaced** — an agent inside a container reads
**host-wide** figures, so it would attribute its neighbours' I/O to itself. That
is the silent-wrong-answer class this program exists to eliminate, so it is
detected rather than tolerated: a non-root cgroup path in `/proc/self/cgroup`
switches the source to the agent's own cgroup.

The cost is real and is stated rather than papered over: **cgroup v2 `io.stat`
carries bytes and I/O counts but no service time**, so `await_ms` and
`queue_depth` are **not derivable in a container** and report `unsupported`.
What *is* available is `io.pressure`, per-cgroup PSI — which the contract already
carries as `stall.io.some` / `stall.io.full`. So a containerized agent keeps a
genuine I/O-stall signal through the existing vitals, just not a latency figure.
No substitute number is invented to fill the gap.

### 6.4 WS-B3 — system-event rule pack (option D)

Curated rules over the existing edge log readers, plus the second stated signal
class — **repeated errors from one service over 24 h**, a rolling per-service
error count evaluated locally. Log evidence is redacted at the edge (ADR-049);
raw lines never leave the host unredacted.

Four rules, all Linux, all read from `journalctl -o json`:

| Signal | Meaning |
|---|---|
| `hung_task` | task blocked > 120 s |
| OOM kill | the kernel killed a process to reclaim memory |
| ATA reset | disk stopped responding, bus reset |
| thermal throttle | the CPU clocked down under thermal load |

A further platform adds rules over its own log reader — the pack is a list of
(source, matcher, meaning) rows, so extending it needs no grammar change.

### 6.5 WS-B4 — edge correlation and curated tunable rules

**Edge correlation.** The KS ranking moves into `mesh-agent-core`. On fire, the
agent ranks its dimensions over the event window against a baseline window and
emits the top-N into the alert's evidence.

**Curated tunable rules.** Extends `ThresholdRule` / `PushAlertRules` /
`AlertRuleProvider` — today a hardcoded three-rule Go literal in
[`DefaultAlertRules`](../../../server/internal/agentapi/alert_rules.go) whose
`StaticAlertRuleProvider.byOrg` override map nothing populates.

**Storage: embedded catalogue + Postgres binding and rollout state.**

| Layer | Home | Mutable at runtime? |
|---|---|---|
| **Rule definition** — predicate, grammar, evidence spec, coverage predicate, `group_by`, `group_window` | Versioned YAML `go:embed`-ed into the server | **No.** Immutable per `(rule_id, version)` |
| **RuleBinding** — customer parameter overrides, keyed `(organization_id, rule_id, selector)` | Postgres | Yes |
| **Rule rollout** — `enabled`, `canary_group`, `rollout_percent`, `kill` | Postgres | Yes |

Rationale, and why not the two obvious alternatives:

- **Definitions in Postgres would move the program's highest-impact gate out of
  CI.** Cost-bounding a predicate before it reaches 5 000 customer endpoints is
  the mitigation for the Risk table's one High-impact row; in CI it is mandatory
  and free, in a runtime API path a validator bug is a production incident.
  It is also worse for evolvability — every grammar extension (§6.5 already
  schedules three) becomes a data migration over every tenant's stored rules,
  where embedded YAML is simply re-analysed at build time.
- **Definitions embedded with no DB layer at all** cannot express a kill switch,
  canary state or coverage. Netdata shipped exactly that, hit the
  "reconfigure every node" wall, and added the DYNCFG override layer on top;
  its precedence is dynamic-override > user file > stock file. Choosing it would
  be choosing a known dead end.
- The split matches where both comparable products converged: Netdata's stock
  `health.d` + DYNCFG overrides, and NinjaOne's fixed catalogue of condition
  *types* with admin-composed instances inside inheritable policies.

**Bindings resolve down the tenancy levels, and carry a tag selector besides.**
`(organization_id, rule_id)` alone gives Contoso one threshold for its whole
estate — while FS01 wants `disk-critical` at 95 and DAL-WS-012 wants 90.

Resolution order, narrowest first:

**device → site → organization → tenant → the embedded default.**

The ordering is not defined here. It is
[`internal/settings`](../../../server/internal/settings/settings.go), shipped by the
tenancy rework: the walk and the tie-break live in one place so they cannot drift
between the things that depend on them, while the *values* stay in
`rule_bindings` where the rule that declares them can validate them. That is why
the file-server-at-95 case needs no special machinery: put the file servers in a
site and set the threshold there.

The kill switch does **not** resolve this way. A customer-wide stop must not be
undone by a value set on one machine, so it reads the ladder the other way up —
`settings.BroadestWins` — which is why it is a column on `rule_rollout` rather
than a binding.

**A binding also carries an optional device-tag selector**, which is a
*cross-cutting* dimension rather than a fourth level — `production` and
`finance` describe machines that span sites. Two tag selectors can match one
machine, so a tag binding carries an **explicit precedence the operator sets and
can see**; every invisible tie-break (newest wins, alphabetical, row order)
produces a threshold nobody can predict from the screen. Within one level, tag
precedence decides; across levels, the narrower level always wins.

This is one column plus a matcher, not an authoring surface; adding it later
would mean rewriting binding resolution and every test that touches it.

This shape is a strict subset of admin-composed instances (the NinjaOne shape),
so growing into that later is an additive migration on `rule_bindings`, not a
rewrite. Nothing here forecloses it.

- **Grammar (I7):** existing comparators, sustain and hysteresis, plus
  rate-of-change, window aggregate (`max`/`mean` over N), and cross-dimension
  conjunction. Statically analysable and cost-boundable before rollout — the
  analysis runs in CI over the embedded catalogue and fails the build. No code,
  no WASM, no side effects.
- **Metric vocabulary alignment.** The rule vocabulary is today `cpu.total`,
  `mem.used`, `disk.used`
  ([alert_breach.go](../../../server/internal/agentapi/alert_breach.go)) while the
  vitals are `cpu.total`, `mem.used_percent`, `disk.used_percent`. The grammar
  adopts the **vitals names** as canonical and accepts the three current names
  as aliases, so rules already pushed to the fleet keep firing across the
  upgrade. A rule naming an unknown metric never fires and is counted
  `unsupported`, never silently skipped. The vocabulary extends to the full
  vitals set, including `disk.await_ms` and `disk.queue_depth` — without them the
  DAL-WS-012 wear-out case (§6.3.1) is collectable but not alertable.
- **Retroactive re-evaluation:** pushing a rule version schedules a bounded,
  resumable, idle-scheduled job evaluating it over local T1 history. Findings
  return as alerts marked `backfilled`. A retro scan yields **one incident per
  `(rule, scope)`**, never N live incidents, so learning a new failure mode does
  not page-flood Contoso's queue.
- **Coverage accounting (I8):** per-rule `active / unsupported / unknown` device
  counts, surfaced in the UI. Silent partial coverage is the exact failure class
  WS-A exists to eliminate.
- **Rollout safety.** Staged by `rollout_percent`, held in Postgres so a stage
  change needs no deploy:

  | Stage | Population | Hold | Advance gate |
  |---|---|---|---|
  | Canary | **max(5 devices, 1 %)** | **1 h** | No per-organization ceiling breach, no agent budget throttle trip, no rule-evaluation error |
  | Staged | **10 %** | **6 h** | Same gates |
  | Full | **100 %** | — | — |

  Any gate failing **halts and reverts to the previous stage automatically**;
  it never advances on a timer alone. Per-agent CPU/IO budget with hard throttle
  (Q6). `kill` is a row flip, effective on the agent's next reconnect and on the
  next rule push, whichever is sooner — no server deploy on the critical path
  for stopping a rule that is degrading customer machines.

### 6.6 WS-C — alerts, evidence and incidents

**Evidence: an immutable compressed blob on the alert row in Postgres.** Zero VM
cardinality; inherits RLS tenancy and the ADR-054 erasure cascade; a frozen
snapshot is what an investigation needs. Hard size cap, validated and redacted
before insert.

**Composition — fixed, not "top-N".** Every bound below is a named constant and
a test:

| Field | Bound | Why |
|---|---|---|
| `ranked[]` | **8** dimensions | The KS ranking's useful tail; beyond 8 the scores are noise |
| `series[]` | the **top 3** ranked dims, event window **±5 min**, **≤ 512 points** each | Enough to see the shape either side of the event at edge resolution |
| `processes[]` | **10** at the event instant | Matches the existing `ProcessReportEntry` rank vocabulary |
| `log_samples[]` | **20** redacted lines | Bounded before redaction, so a flood cannot defeat the cap |
| Total | **≤ 64 KB compressed** (Q8) | Truncate with `truncated: true`; never reject (E11) |

**Codec: DEFLATE**, `evidence_codec = "deflate-1"`. `flate2` is already in the
agent lock (`edge-tsdb`'s cold tier) and Go's `compress/flate` reads it from
stdlib, so this adds **no dependency on either side**. The codec is a versioned
string on the row, so a future change is additive rather than a rewrite.

**Volume at Contoso** *(derived — Q12 measures it)*: at 0.2 alerts/device/day and
~5 KB compressed — ~1 000 alerts/day, ~1.8 GB/year. The rate is an estimate, and
the ceilings below, the retention deferral (§14.3) and one of §14.2's revisit
triggers all rest on it, so §9.1.2 turns it into a soak measurement with the
response escalated rather than assumed.

**Severity — closed set:** `info | warning | critical`. Declared per rule,
overridable per binding. No numeric priority: NinjaOne carries severity *and*
priority because its conditions drive ticketing and automation, which §4.2
excludes here.

**Alert-rate ceilings (Q9, E9).**

| Ceiling | Value | Enforced at | Accounting |
|---|---|---|---|
| Per device | **20 alerts/h** | Agent | Counted locally, reported in the next summary, aggregated to `opengate_alerts_suppressed_total{reason="agent_ceiling"}` |
| Per organization | **500 alerts/h** | Server | `opengate_alerts_suppressed_total{reason="organization_ceiling"}` |

**The ceiling is per customer, not per MSP.** At the tenant it would let Contoso's
bad night consume the budget of every other customer Northwind looks after —
one customer's storm silencing detection across the estate is a worse failure
than the storm.

500/h is ~12× Contoso's derived steady rate (~42/h), so it catches a storm
without clipping normal operation. Excess folds into **one** storm incident
carrying a count — suppression is always counted, never silent (I1).

**Incident engine.** Grouping per §7.3; lifecycle
`new → acknowledged → investigating → resolved`. An incident in `new` **is**
the triage queue — which is why no separate promotion entity exists.

**`reopen_window` defaults to the rule's own `group_window`,** overridable per
rule. This is definitional rather than arbitrary: an incident must stay open
exactly as long as a new alert could still fold into it, otherwise auto-resolve
and grouping disagree and WS-4471's 30 daily freezes fragment into 30 incidents.
So a fleet-event rule auto-resolves 30 min after its last alert, a slow burn
after 24 h, a recurrence rule after 7 d.

**Cause code — closed set,** required on manual resolution:
`resolved_self | fixed_by_tech | hardware_fault | expected_load |
false_positive | duplicate | wont_fix`. `false_positive` is load-bearing: it is
the feedback channel that tells you which curated rule needs its threshold moved.

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
| `Vital` | agent → VM | `(tenant, organization, device, name)` | 30 d |
| `Rule` | platform, **embedded in the server** | `(rule_id, version)` | immutable per version; lifetime = the release |
| `RuleBinding` | server, **Postgres** | `(organization, rule_id, selector)` | customer parameter overrides |
| `RuleRollout` | server, **Postgres** | `(organization, rule_id)` | `enabled` / `canary_group` / `rollout_percent` / `kill` |
| `RuleCoverage` | server | `(rule_id, version, device)` | activation state |
| `Alert` | agent → server | `(device, rule_id, rule_version, window_start)` | 1 y, immutable |
| `Evidence` | agent → server | 1:1 with `Alert` | erased with its alert |
| `Incident` | server | `(organization, rule_id, scope, scope_key)` while open | 1 y |
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

An `Alert` joins an open `Incident` when **all** hold: same `organization_id`; same
`rule_id` (**not** `rule_version` — a rule upgrade must not fork a live incident);
scope-compatible per the rule's `group_by`; and
`now - incident.last_seen <= rule.group_window`.

**`organization` is the broadest grouping scope there is.** A rule may not group
at the tenant, because that would fold Contoso's driver rollout and Fabrikam's
unrelated outage into one incident — two customers, one room, no correct
assignee. Grouping never crosses a customer boundary.

| Rule shape | `group_by` | `group_window` | RMM effect |
|---|---|---|---|
| Fleet event | `organization` / `site` | 30 min | Contoso's driver rollout: 312 alerts → **1** incident, 40 devices |
| Slow burn | `device` | 24 h | FS01 disk filling: re-fires fold into one incident |
| **Recurrence** | `device` | 7 d | WS-4471: 30 daily freezes → **1** incident, "30 occurrences" |

The recurrence row is load-bearing: for WS-4471 **the pattern is the diagnosis**,
and only grouping across *time* makes it visible.

Enforced by a partial unique index on `(organization_id, rule_id, scope,
scope_key) WHERE status <> 'resolved'`, which makes the fold race-safe.

### 7.4 Schema (migrations `013_rules`, `014_investigations`)

Two migrations, not one, because the rule tables land with WS-B step 12 while
the investigation tables land with WS-C step 16. The tenancy rework took 010
through 012 ([migrations/](../../../server/internal/db/migrations/)), so 013 is the
next free number; confirm it at implementation time rather than trusting this
line.

**`014_investigations`:**

Every table below carries **both** `tenant_id` and `organization_id` (§3.2):
isolation is enforced on the first, all scoping keys on the second.

`alerts` — `id, tenant_id, organization_id, device_id → devices(id) ON DELETE
CASCADE, rule_id, rule_version, severity, metric, value, window_start,
window_end, observed_at, received_at, backfilled, incident_id, evidence bytea,
evidence_codec`, with `UNIQUE (device_id, rule_id, rule_version, window_start)`
as the idempotency key.

`incidents` — `id, tenant_id, organization_id, rule_id, scope, scope_key,
severity, status, assignee_id, opened_at, first_seen, last_seen, resolved_at,
cause_code, occurrences, device_count`.

`incident_events` — `id, tenant_id, organization_id, incident_id → incidents(id) ON DELETE CASCADE,
at, kind, actor_id, body jsonb`, `kind ∈ {alert_folded, status_change, assignment,
comment, device_offline, resolution}`.

**`013_rules`:**

`rule_bindings` — `id, tenant_id, organization_id, rule_id, level, level_key,
selector jsonb, precedence, params jsonb, updated_at, updated_by`, with
`UNIQUE (organization_id, rule_id, level, level_key, selector)`. `level` is one
of `device | site | organization` and `level_key` names the row at that level;
`selector` is a bounded tag predicate; `precedence` breaks ties between two tag
selectors at the same level (§6.5); `params` holds only values the rule declares
tunable, validated against the embedded rule's declared bounds on write.

`rule_rollout` — `tenant_id, organization_id, rule_id, enabled, canary_group,
rollout_percent, kill, stage_entered_at, updated_at, updated_by`,
`PRIMARY KEY (organization_id, rule_id)`.

All five: forced RLS **on `tenant_id`**, with `tenant_id`-leading indexes for
isolation and `organization_id`-leading indexes for the scoping reads, mirroring
the `device_inventory` pattern. `cause_code` and `severity` are constrained to the §6.6 closed sets by
check constraints, not application convention.

### 7.5 Wire contract

Additive, behind a new `Alerts` capability, golden-file tested both directions;
`controlFieldCount` bumped from its **current 86**
([control_encode.go](../../../server/internal/protocol/control_encode.go)) with a
per-field encoder arm. Read the constant at implementation time rather than
trusting this figure — it moves with every protocol addition.

`AgentAlert` is the **single** alert transport (D22). The existing
`AlertBreach`-on-`AgentHealthSummary` path continues unchanged for the duration
of this program because §14.2 defers touching the series it feeds; folding the
two into one path is part of that revisit, not this one. Until then the alert
*store* has exactly one writer — `AgentAlert` — so there is no second source of
truth for incidents.

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
- **I6 — tenancy.** Every read and write tenant-isolated through RLS, and
  organization-scoped by query. Two mechanisms, two questions: the first is a
  wall, the second is which customer you are looking at.
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
| Rust | `mesh-agent-core::ml::pressure` **(new)** | PSI reader, cgroup-scoped when containerized |
| Rust | `mesh-agent-core::ml::diskperf` **(new)** | `/proc/diskstats` reader, `/sys/block/` device filter, worst-device reduction, cgroup-v2 fallback and environment detection |
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
| Go | `internal/rules` **(new)** | embedded YAML catalogue, CI cost-bound analysis, binding/selector resolution, rollout state |
| DB | `013_rules` | `rule_bindings`, `rule_rollout` |
| DB | `014_investigations` | `alerts`, `incidents`, `incident_events` |
| Web | `features/investigations/` **(new)** | list + detail; routes `/investigations`, `/investigations/:id` |
| Web | `features/devices/DeviceMetrics.tsx` | 60 s vitals; drag-to-correlate retired |
| Deploy | `monitoring/values.yaml` | retention 30 d. VM resources **unchanged pending the Q3 measurement** (§9.1); soak dashboard unchanged (§14.2) |

---

## 9. Non-functional requirements and quality metrics

### 9.1 Performance

| # | Budget | Target |
|---|---|---|
| Q1 | Central **vitals** series per device | **≤ 24**, test-enforced. Scope is the vitals set — settled, not pending (§14.2) |
| Q2 | Vitals active series at 5 000 agents | **≤ 120 000** — *derived as Q1 × fleet*, not an independent number, so the two can never drift. A Linux host now uses the full 24, so the worst case **is** the budget (§2.3) — there is no slack between them |
| Q3 | **VM RAM per active series** | **measured, not assumed** — the ~2 KB figure becomes an experiment; budget ≤ 400 MB total |
| Q4 | VM disk at 5 000 agents, 30 d | ≤ 2 GB (measured projection 1.43 GB) |
| Q5 | Agent CPU, steady state | < 1 % of one core |
| Q6 | Agent CPU, retroactive job | < 5 %, hard-throttled, idle-scheduled |
| Q7 | Agent local store | ≤ 512 MB, never fills the host disk |
| Q8 | Evidence size | ≤ 64 KB compressed per alert |
| Q9 | Alert rate | per-device and per-organization ceilings with storm suppression |
| Q10 | Incident list p99 | ≤ 200 ms at 10 000 open incidents |
| Q11 | **Retroactive reach at T1** | **measured on a steady-state store**, not derived from cap ÷ density |
| Q12 | **Alert rate per device** | **measured over a soak**, not the 0.2/device/day estimate |

**VictoriaMetrics is sized after Q3 measures — not before.** The live limit is
**512 Mi** (§2.2) and **stays there** until the experiment produces a number.

The reason for not pre-raising: the whole point of Q3 is that ~2 KB/series is a
rule of thumb this plan already caught being cited as if measured (§1.5). Sizing
the pod against that same unmeasured figure would re-commit the error one layer
down — picking a limit from the number the experiment exists to replace.

What is at stake, so the measurement is read against a stated expectation:

| Measured RAM/series | Requirement at Q2's 120 000 | Against the live 512 Mi |
|---|---|---|
| ~2 KB *(the derived figure)* | ~240 MB | Fits, ~2.2× headroom |
| ~3 KB | ~360 MB | Fits, but inside the Q3 budget's margin |
| ~4 KB | ~480 MB | Nominally fits, no headroom — one bad day from an OOM kill |
| > 4.5 KB | > 540 MB | Exceeds the limit |

**The response is an explicit decision point, not a rule applied automatically.**
Step 6 runs the experiment and **stops with the number**; the options below go to
the project owner rather than being resolved by whoever is implementing:

1. **Raise the limit** to fit the measurement with headroom (cluster has room —
   §2.2's volume ceiling is a *disk* constraint, not memory).
2. **Shrink the vitals set** below the 24 cap so the measurement fits 512 Mi —
   keeps the cap as the invariant and memory as the consequence.
3. **Lower the fleet target** for this contract, re-deriving Q2.
4. **Accept the measurement as-is** if it lands comfortably (~2 KB), changing
   nothing.

Recording the trade-off rather than the answer is deliberate: which of these is
right depends on a number that does not exist yet, and only one of them is
reversible cheaply once agents are shipping at the new cadence.

### 9.1.1 Q11 — retroactive reach, measured

**Answered (EF-B10): ~7 months.** A store is driven through the production write
path until its cap is evicting, then asked for the oldest minute it still holds
([reach_test.rs](../../../agent/crates/mesh-agent-core/tests/reach_test.rs)). Run at
three cap sizes across a fourfold range, the reach scales with the cap to within
5 %, which is what makes extrapolating to the shipped 512 MB cap evidence rather
than the same division in a new coat:

| Cap | Measured T1 reach | Density |
|---|---|---|
| 2 MiB | 18.7 h | 2.39 B per stored reading |
| 4 MiB | 39.2 h | 2.28 B per stored reading |
| 8 MiB | 79.7 h | 2.25 B per stored reading |
| **512 MiB (shipped)** | **~5 100 h ≈ 213 d** | extrapolated, linearity within 5 % |

**The shape this assumed** is the vitals set as it ships: 13 series at 1 Hz on a
machine doing real work. Reach follows the shape — the deferred per-entity
collectors program (§4.2) multiplies what the store holds and divides the reach
by the same factor — so the number is only meaningful next to it, and the
measurement reports its own density beside it for exactly that reason.

Against the decision table this was to be read against: the answer is **months**,
so §5.4's verdict holds — the device reaches back two orders of magnitude further
than the 48 h a central flight recorder would have kept — while "years at 60 s"
is **restated as measured** in §2.6, §5.4 and here. No owner decision is
outstanding: `EDGE_STORE_CAP_MB` is unchanged, tier retention is unchanged, and
the retroactive claim stands as "months of history, on the device".

### 9.1.2 Q12 — alert rate is measured, not estimated

**0.2 alerts/device/day is an estimate**, labelled *(derived)* in §6.6, and three
separate decisions currently rest on it: the ~1.8 GB/year evidence projection,
the 500/organization/h ceiling (D28, set as ~12× a rate that was never observed), and one
of §14.2's two revisit triggers. §14.2 already names "measured alert volume at
Contoso scale" as a precondition — this makes it a gate rather than an
aspiration.

| Measured rate | Contoso/day | Evidence/year | Consequence |
|---|---|---|---|
| **0.2/device/day** *(assumed)* | 1 000 | ~1.8 GB | Ceilings hold with ~12× headroom; §14.3's deferred sweep stays safe |
| 1/device/day | 5 000 | ~9 GB | Headroom falls to ~2.4×; retention sweep needed sooner than "a later program" |
| 5/device/day | 25 000 | ~45 GB | **Org ceiling clips normal operation**; retention urgent; triage queue unusable without tighter grouping |

**Owner decides** between: accept and proceed; raise the ceilings and pull
§14.3's retention sweep forward; tighten curated thresholds or ship a smaller
rule pack; or strengthen grouping so incident count stays usable at the measured
alert count.

Measure it on the soak before the rule pack goes past its canary stage — a rate
discovered after full rollout is discovered as an incident.

### 9.2 Security

- **Tenancy (I6):** RLS on `tenant_id` exactly as `device_inventory`; cross-tenant
  regression tests including crafted grouping keys. Separately, cross-**organization**
  leakage is a query defect rather than a wall breach and gets its own tests — a
  Contoso-scoped read must never return a Fabrikam row even though both sit inside
  one tenant.
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
| E9 | Alert storm across Contoso | Per-device and per-organization ceilings; excess folded into one storm incident **with a count**; never silently dropped |
| E10 | Bad rule degrades customer machines | Per-agent budget + throttle; canary; kill switch on reconnect |
| E11 | Evidence exceeds the cap | Truncated at the edge with `truncated: true`; never rejected wholesale |
| E12 | Evidence contains a secret or raw log line | Redacted at the edge; asserted by test |
| E13 | DAL-WS-012 purged mid-incident | Its alerts and evidence erased; incident survives minus that device; empty incident closed |
| E14 | Organization deleted | Full cascade; no orphan incidents. A tenant purge cascades every organization beneath it |
| E15 | Two rules fire on one condition | Distinct incidents unless `group_by` says otherwise |
| E16 | Rule upgraded while an incident is open | Grouping keys on `rule_id`; the live incident does not fork |
| E17 | Fleet event where no host individually breaches | Low-severity **observations** fold into an incident only on cross-device co-occurrence |
| E18 | VM unavailable | Vitals reads degrade to 503; **alerts and incidents keep working** |
| E19 | Postgres unavailable | Alert buffered at the edge, retried on reconnect; never acknowledged as stored when it is not |
| E20 | Agent local store corrupt | Recreate rather than crash; coverage reports `unknown` until rebuilt |
| E21 | Host disk nearly full | Free-space backoff shrinks the cap; retroactive jobs suspend first |
| E22 | Newly enrolled device | Rules activate immediately; retroactive scope empty — reported honestly |
| E23 | **FS01-shaped multi-volume host** | Worst-mount reduction; `disk.mounts_critical` ≥ 1 makes the 98 % volume visible |
| E26 | **Agent runs inside a container** | Source switches to the agent's own cgroup; `await_ms` / `queue_depth` report `unsupported`; `stall.io.*` still measured from `io.pressure`. **Host-wide `/proc/diskstats` is never reported as the container's own** |
| E27 | **Guest disk stalls on host contention or an IOPS cap** | Reported as the guest observes it — the latency the customer experiences is the true reading, not an error |
| E28 | **Device disappears mid-window** (hot-unplug, volume detach) | Dropped from the reduction; no rate computed across the gap, matching `byte_rate`'s existing never-a-wrong-number contract |
| E29 | **Counter wrap or reset in `/proc/diskstats`** | Sample yields `None` rather than a negative or huge rate |
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
6. **Containerized agents have no disk-latency vital.** cgroup v2 `io.stat`
   carries no service time, and no substitute is invented. `stall.io.*` from the
   cgroup's `io.pressure` carries the I/O signal instead (§6.3.2).
7. **Disk performance comes from `/proc/diskstats`**, so a platform without an
   equivalent would report capacity but not latency or queue depth.

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
- B2 At Contoso scale the vitals set measures ≤ 120 000 active series and
  ≤ 2 GB / 30 d *(Q2, Q4)*. The Q3 RAM-per-series figure is **measured and
  recorded**; exceeding the 400 MB budget does not fail the step — it triggers
  §9.1's sizing decision, which is the owner's.
- B3 **FS01 fixture** (120 GB @ 98 %, 2 TB @ 10 %) reports `disk.used_percent`
  ≈ 98 and `disk.mounts_critical` = 1 — the current code returns ≈ 15 and 0.
- B4 A 5 s CPU-pinning stall inside a 60 s bucket yields `cpu.total.max` ≈ 100
  while `avg` stays < 30.
- B5 Stall vitals equal the kernel's `avg60` within tolerance.
- B11 On a host without `/proc/pressure`, stall vitals are **absent** (not zero)
  and every rule watching them reports `unsupported`.
- B15 **Worst-device reduction**: a fixture with one device at 40 ms await and
  another at 2 ms reports `disk.await_ms` = 40, not the mean — and the
  `queue_depth` worst device is selected independently of the `await` one.
- B16 A 5 s I/O stall inside a 60 s bucket yields `disk.await_ms.max` ≈ the
  stall latency while `disk.await_ms` stays low *(the disk analogue of B4)*.
- B17 **Device filter**: a fixture containing `nvme0n1`, `nvme0n1p1`, `vda`,
  `loop0`, `ram0` and `dm-0` counts `nvme0n1`, `vda` and `dm-0` exactly once
  each, and excludes the partition and the pseudo-devices.
- B18 **Container**: with a non-root cgroup path, `await_ms` and `queue_depth`
  report `unsupported`, `stall.io.*` are read from the cgroup's `io.pressure`,
  and **host-wide `/proc/diskstats` values never appear** *(E26)*.
- B19 **VM**: a guest fixture exposing `vda` produces all three vitals, and a
  counter wrap or a mid-window device disappearance yields no sample rather
  than a wrong one *(E28, E29)*.
- B6 Each event rule fires exactly once per matching event and never on a
  non-matching one.
- B7 Edge correlation ranks a synthetic broken-pattern dimension first, matching
  the Go engine's ranking on the same fixture *(port-equivalence)*.
- B8 A pushed rule reports `active + unsupported + unknown == fleet size`.
- B9 A retroactive scan over local history produces the expected alerts, marked
  `backfilled`, in **one** incident per `(rule, scope)`.
- B10 Retroactive job stays within Q6 and suspends under disk pressure.
- B14 **Q11 measured**: T1 reach on a steady-state store is recorded from a real
  store, not computed from cap ÷ density. A reach under 48 h stops for the
  §9.1.1 decision rather than proceeding.
- B12 **Rule storage**: the CI cost-bound analysis fails the build on a
  catalogue rule exceeding the per-agent budget; a binding whose `params` fall
  outside the rule's declared bounds is rejected on write; selector resolution
  is most-specific-wins, proven with FS01 (95) and DAL-WS-012 (90) under one
  Contoso `disk-critical` rule; a rule pushed under a legacy metric name
  (`mem.used`, `disk.used`) still fires after the vitals rename.
- B13 **Rollout**: a canary stage that trips the organization ceiling or the agent budget
  reverts to the previous stage and does not advance on the hold timer;
  `kill = true` stops evaluation on the next reconnect **without a deploy**.

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
- C7 Cross-tenant read denied, including via a crafted grouping key. Cross-organization
   reads inside one tenant are denied by scoping, proven with Contoso and Fabrikam data
   present together.
- C8 Purging DAL-WS-012 erases its alerts and evidence, leaves the incident with
   `device_count = 39` (E13).
- C9 Evidence over the cap is truncated with `truncated: true`.
- C14 Evidence composition matches §6.6 exactly — 8 ranked dims, 3 series of
   ≤ 512 points, 10 processes, 20 log lines — and a DEFLATE blob written by the
   agent round-trips through `compress/flate` on the server.
- C15 **Q12 measured**: alerts/device/day is recorded from the soak before the
   rule pack advances past canary, and the §9.1.2 decision is taken with the
   number in hand rather than the estimate.
- C10 No evidence field can carry an unredacted log line or cmdline.
- C11 Aggregate `/metrics` counters are O(rules): fleet size does not change
   series count.
- C12 Incident list p99 ≤ 200 ms at 10 000 open incidents.
- C13 Golden round-trip for the new control pair passes both directions.

**Web (step 21)**
- W1 `/investigations` lists open incidents with status, severity, rule,
  `occurrences`, `device_count`, first/last seen; filters by status, severity,
  rule and device; keyset pagination.
- W2 `/investigations/:id` shows the timeline, the folded alerts, and each
  alert's evidence — ranked dimensions, the three series, processes, redacted
  log lines — rendered from the frozen snapshot, with **no** call back to the
  device.
- W3 Status transitions, assignment and comments are issued from the room;
  resolving requires a cause code from the closed set; an illegal transition is
  not offerable in the UI.
- W4 The device page carries an **incidents strip** linking into the room.
- W5 A truncated evidence blob says so; an incident whose device was purged
  renders without it, not as an error.
- W6 Per-rule coverage is visible — I8's "surfaced in the UI". Silent partial
  coverage is the failure class WS-A exists to eliminate, and a number nobody
  can see is silent.

**Gaps found while decomposing this plan, and where each closed**

Four things this section originally described a smaller program than. Each was
resolved inside a micro-plan rather than deferred:

1. **Coverage had no wire transport** (§7.5) — `rule_coverage[]` rides
   `AgentHealthSummary` additively, so an agent that predates it sends the
   byte-identical frame it always did (EF-B8,
   [ADR-070](../../../docs/adr/ADR-070-rule-grammar-and-coverage.md)).
2. **`RuleCoverage` had no storage** (§7.4) — coverage is liveness, so states are
   held per connected agent and `unknown` is derived as fleet minus reported;
   only "this machine cannot evaluate this rule at all" is durable (EF-B8).
3. **Evidence's 64 KB cap collided with `maxTelemetryPayloadBytes`** — the alert
   payload carries its own bound rather than borrowing the telemetry one (EF-C1,
   [ADR-074](../../../docs/adr/ADR-074-alert-store-accounted-ingest-and-the-erasure-cascade.md)).
4. **The web step had no acceptance criterion** — W1–W6 above are it (EF-C6,
   [ADR-078](../../../docs/adr/ADR-078-the-triage-workspace-reads-a-snapshot.md)).

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
   10 s stream removal; **run the Q3 RAM-per-series experiment and stop with the
   number** *(B2, Q3)*. VM resources are left at 512 Mi; §9.1's four sizing
   options go to the project owner before any limit changes.
7. PSI reader + stall vitals, **Linux only**, cgroup-scoped when containerized,
   with `unsupported` coverage on non-PSI hosts *(B5, B11)*.
8. **Disk-performance reader**: `/proc/diskstats`, `/sys/block/` device filter,
   worst-device reduction, container and VM handling *(B15–B19)*.
9. Event rule pack + per-service 24 h error rule *(B6)*.
10. Port KS ranking to `mesh-agent-core`, prove port-equivalence, retire
    `internal/correlate` and the drag UX *(B7)*. `/correlate` is **removed from
    the OpenAPI spec outright**, not deprecated — it has no external consumers,
    and the web client's only caller is the drag UX retiring in the same step.
11. Rule grammar extension + metric-alias mapping + coverage accounting *(B8)*.
12. **Rule storage**: embedded YAML catalogue + CI cost-bound analysis; migration
    `013_rules` (`rule_bindings`, `rule_rollout`) with selector resolution *(B12)*.
13. Retroactive evaluation job + budget/suspend behaviour *(B9, B10)*.
    **Q11 reach measured on a steady-state store** *(B14)*: ~7 months, so §5.4's
    verdict holds and "years" is restated as measured (§9.1.1).
14. Rollout safety: staged gates, throttle, ceilings, kill switch *(B13)*. This
    builds the **machinery**; the fleet rollout of the curated pack past canary
    waits on Q12 at step 19, since alerts only reach a store from step 16 and
    the counters that measure the rate land at 19.

**WS-C**
15. Wire contract + capability + goldens *(C13)*.
16. Migration `014_investigations`, RLS store, ingest with I1 accounting
    *(C1, C7)*.
17. Incident engine: grouping, lifecycle, `reopen_window` auto-resolve *(C2–C6)*.
18. Erasure cascade *(C8)*; evidence composition, caps and redaction
    *(C9, C10, C14)*.
19. Aggregate `/metrics` *(C11)*. **Not blocked** — §14.2 leaves the soak
    dashboard untouched. These counters are what **measures Q12** *(C15)*; the
    soak runs before the rule pack advances past canary, and §9.1.2's options go
    to the owner with the number.
20. API + OpenAPI + Go/TS regeneration *(C12)*.
21. Web investigations feature; device page incidents strip; chart on vitals.

**Close-out**
22. Budget measurements captured as evidence; E2E; docs; ADRs +
    `decisions.md` rows; `phases.md`; **tech-debt rows for §14.2 and §14.3**;
    archive this plan.

### 13.1 Micro-plan index

This document is the **master plan + micro-plan index**. Each execution spec is a sibling
`edge-first-*.md` in this directory; completed specs move to `archive/`. Plans reference other
plans by plain path (never a markdown link) — only repo source and docs are linked.

Every step above is owned by exactly one micro-plan, and every §12 acceptance criterion by exactly
one micro-plan.

| Micro-plan | Steps | Criteria | Depends on |
|---|---|---|---|
| `archive/edge-first-a1-telemetry-accounting.md` | 1, 3 | A1, A2, A4 | — |
| `archive/edge-first-a2-honest-grids-and-retention.md` | 2, 4 | A3, A5 | — |
| `archive/edge-first-b1-disk-worst-mount-reduction.md` | 5 | B3 | — |
| `archive/edge-first-b2-vitals-contract-and-cadence.md` | 6 (code) | B1, B4 | B1 |
| `archive/edge-first-b3-vm-series-cost-measurement.md` | 6 (measurement) | B2 | B2 |
| `archive/edge-first-b4-stall-vitals-psi.md` | 7 | B5, B11 | B2 |
| `archive/edge-first-b5-disk-performance-vitals.md` | 8 | B15–B19 + the exact 24-series count | B2, B4 |
| `archive/edge-first-b6-event-rule-pack.md` | 9 | B6 | — |
| `archive/edge-first-b7-edge-correlation-port.md` | 10 | B7 | — |
| `archive/edge-first-b8-rule-grammar-and-coverage.md` | 11 | B8 | B2, B4, B5 |
| `archive/edge-first-b9-rule-catalogue-and-bindings.md` | 12 | B12 | B8 |
| `archive/edge-first-b10-retroactive-evaluation.md` | 13 | B9, B10, B14 | B6, B8, B9 |
| `archive/edge-first-b11-rollout-safety.md` | 14 | B13 | B9 (+ C2 signal) |
| `archive/edge-first-c1-alert-wire-and-evidence.md` | 15, 18 (evidence) | C9, C10, C13, C14 | B6, B7 |
| `archive/edge-first-c2-alert-store-and-ingest.md` | 16, 18 (erasure) | C1, C7, C8 | C1 |
| `archive/edge-first-c3-incident-engine.md` | 17 | C2–C6 | C2 |
| `archive/edge-first-c4-aggregate-metrics-and-q12.md` | 19 | C11, C15 | C2, C3, B8 |
| `archive/edge-first-c5-investigations-api.md` | 20 | C12 | C3, B8, B9 |
| `archive/edge-first-c6-investigations-web.md` | 21 | W1–W6 | C5, B2 |
| `edge-first-z1-close-out.md` | 22 | — | all |

**Parallelisable now:** `a1`, `a2`, `b1`, `b6`, `b7` share no files. `b2` waits on `b1`; the
`b4`/`b5` pair and `b8`→`b9`→`b11` are chains; all of WS-C is a chain from `c1`.

**Two coordination points:** `b8` and `c1` both add wire fields and both bump `controlFieldCount`
(**86** today) — whoever lands second rebases *then* regenerates goldens. `b9` takes migration
`010`, `c2` takes `011` — both confirm the next free number at implementation time.

---

## 14. Settled deferrals

Not open questions. Each is a decision to **not act now**, with the scope of the
non-action fixed and a named trigger for revisiting. Nothing in this section
blocks any implementation step.

### 14.1 Stall-vitals platform scope — **SETTLED: Linux only**

`stall.*` ships on Linux from `/proc/pressure/*` and nowhere else. The agent
implements Linux, so the decision binds nothing today — it is recorded because it
governs the moment a second platform arrives: no analogue is synthesized from
counters that measure different things in different units, since that puts two
meanings behind one name. A platform without PSI keeps `cpu.total.max`, its own
event rules and the disk reductions; what it lacks is only the continuous stall
gauge, reported as `unsupported` (I8), never implied. Full reasoning is in §6.3.

**Revisit if** a platform with a genuine time-in-stall primitive gains an agent,
or the deferred per-entity collectors program (§4.2) builds a contention set —
which would ship under **its own names**, never as `stall.*`.

### 14.2 Alert state in VictoriaMetrics — **SETTLED: defer, change nothing**

The three existing edge series that are "detail" by the O(1) rule —
`opengate_edge_alert_breach{rule,metric}` and
`opengate_edge_process_{cpu,mem}_percent{rank}` — are **left exactly as they
are**. §5.6 analysed four shapes and none is satisfying today: keeping them
breaches the O(1) rule and charts a semantically-shifting line; an agent-reported
count creates two disagreeing answers; a server-derived projection cannot
cross-check its own source; removing them costs the only external observation
path for a bad rollout. That analysis stands and is not to be re-derived.

Deferring is the right call because the decision is **cheap to make later and
expensive to make wrong now** — the information that would settle it does not
exist yet, and acting early would either delete an observation path still in use
or entrench series that the incident API may make redundant.

**Scope of the non-action, so this cannot drift:**

- No new per-device alert series are added by this program.
- The three existing series are not renamed, relabelled, or removed.
- The soak dashboard is untouched; step 19 is **not blocked** by this item.
- §6.7's aggregate O(rules) server metrics land regardless — they are
  fleet-aggregate, contribute no per-device series, and stand whatever this
  resolves to.
- **Q1 is enforced over the vitals set.** That is now a decision, not a
  placeholder: the vitals contract is what the cap exists to protect, and the
  WS-19 breach series are governed by their own rule-count bound.

**Revisit when both** are known: the real fleet-board query shape (once the
incident engine and its API exist and a tech has used them), and measured alert
volume at Contoso scale rather than §6.6's 0.2/device/day estimate. Both convert
the trade from hypothetical to arithmetic.

### 14.3 Retention enforcement — **SETTLED: policy declared, sweep deferred**

Alerts, evidence and incidents are **declared** 1 y. No age-based deletion ships
in this program: existing purge machinery in
[`internal/lifecycle`](../../../server/internal/lifecycle/) is device- and
org-triggered, and adding a scheduled sweep is a self-contained piece of work
that nothing else here depends on. At §6.6's derived ~1.8 GB/year the cost of
waiting is small and bounded.

Two things make the deferral safe rather than sloppy, and both are in scope now:

- **Erasure still works.** I9's cascade means a purged device or org takes its
  alerts and evidence with it (C8, E13/E14), so the ADR-054 obligation is met
  without the sweep.
- **Growth is observable.** The aggregate `/metrics` in §6.7 make row growth
  visible, so the sweep gets built against a measured rate.

**This is recorded as a tech-debt row at close-out (step 22)** — a declared
retention with no mechanism is exactly the silent-drift class WS-A exists to
eliminate, and it is not allowed to survive only in this plan.

**Revisit when** measured growth projects past ~10 GB/year, or a compliance
commitment makes 1 y contractual rather than aspirational.

---

## 15. Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Removing the 10 s stream reads as a regression | High | Medium | Deliberate; extrema preserve event visibility; docs state current behaviour |
| A bad curated rule degrades 5 000 machines | Low | **High** | Declarative grammar, static cost analysis, canary, throttle, kill switch |
| Alert storm floods Postgres | Medium | High | Per-device/per-organization ceilings, storm folding with counts, evidence cap |
| Grouping merges unrelated events | Medium | Medium | Keyed on `rule_id` + scope + window; E15 |
| Grouping fragments one event | Medium | Medium | Per-rule `group_window`; C2/C3 fixtures |
| Silent partial rule coverage | Medium | High | I8 — the WS-A lesson applied forward |
| Retroactive job harms a customer machine | Low | High | Idle-scheduled, throttled, suspends first |
| Vitals set creeps past O(1) | Medium | High | Q1 cap test |
| Evidence leaks PII or secrets | Low | High | Edge redaction + test + pen-test gate |
| Disk-semantics change surprises existing rules | Medium | Medium | Called out in ADR + docs; B3 fixture pins both values |
| Edge correlate diverges from the Go engine | Medium | Low | B7 port-equivalence test on shared fixtures |
| Scope creep into notifications/remediation | High | Medium | §4.2 forbids both; ADR records the deferral |
| Deferred retention sweep forgotten, tables grow unbounded | Medium | Medium | D25 — tech-debt row is a close-out deliverable, not a note in this plan; §6.7 counters make growth observable; ~1.8 GB/y bounds the cost of being late |
| §14.2 deferral drifts into "never decided" | Medium | Low | Revisit trigger is named and measurable (incident API in use + measured alert volume); tech-debt row at close-out |
| A future non-PSI platform reads as second-class without stall vitals | Low | Low | D23 — B4 proves `cpu.total.max` catches the motivating freeze; each platform brings its own event rules; coverage reports `unsupported` rather than implying |
| A binding selector matches unintended devices | Medium | Medium | Most-specific-wins resolution, bounded predicate grammar, B12 fixture over FS01 and DAL-WS-012 |
| VM sizing left open while the fleet grows | Low | Medium | D32 — the fleet is one device today (§2.2), so runway is long; step 6 stops with the number well before Contoso scale, and Q1's cap bounds the worst case at 24/device regardless |
| Measured reach falls far short of "years", weakening §5.4 | Medium | Medium | **Closed at step 13**: measured at ~7 months, two orders of magnitude past the 48 h a central recorder would have kept, so §5.4's verdict holds and "years" is restated as measured (§9.1.1) |
| Containerized agent reports host disk I/O as its own | Medium | **High** | E26/B18 — cgroup detection switches the source and reports `unsupported` rather than a host-wide number; this is the silent-wrong-answer class WS-A exists to eliminate |
| Vitals cap now has zero headroom | High | Low | Deliberate (§6.2) — the next vital re-opens D3, which is the friction the cap exists to create |
| Real alert rate is many times the estimate | Medium | High | D35/Q12 — measured on the soak *before* the pack leaves canary, so a rate that would clip the organization ceiling or flood triage surfaces while the population is still small |

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
| D16 | **Aggregate O(rules) server metrics** for platform meta-monitoring. The three existing per-device WS-19 edge series are **left unchanged**; deferred with a named revisit trigger | §6.7, §14.2 |
| D17 | Evidence is an **immutable compressed blob on the alert row in Postgres** | §6.6 |
| D18 | **Incident is the room**; status lifecycle is the triage queue | §7 |
| D19 | Grouping is **two-axis** — declared scope × declared window; recurrence across time required | §7.3 |
| D20 | Target **5 000 agents**; vitals 30 d; alerts and investigations 1 y | §3 |
| D21 | **No notifications, no remediation actions** in this program | §4.2 |
| D22 | WS-C **extends** the WS-19 alert path; there is one alerting system, and `AgentAlert` is the alert store's only writer | §1.3, §7.5, §9.3 |
| D23 | **Stall vitals are Linux-only.** No analogue is synthesized for a platform without a time-in-stall primitive — counters that measure different things under one name are the FS01 defect. Binds nothing today (the agent implements Linux); governs the next platform | §6.3, §14.1 |
| D24 | **Rules: embedded catalogue + Postgres bindings/rollout.** Definitions are CI-analysed and immutable per version; bindings resolve device → site → organization with a cross-cutting tag selector from day one; `kill` is a row flip, not a deploy | §6.5 |
| D25 | **1 y retention is declared, its sweep deferred.** Erasure still cascades; growth is observable; recorded as tech debt at close-out | §14.3 |
| D26 | **Clock bounds fixed:** `maxBacklog` 7 d, `maxSkew` 5 min | §6.1 |
| D27 | **`reopen_window` defaults to the rule's `group_window`** — definitional, so auto-resolve and grouping can never disagree | §6.6 |
| D28 | **Ceilings fixed:** 20 alerts/device/h at the edge, 500/organization/h at the server — per customer, never per tenant; both suppressions counted | §6.6 |
| D29 | **Evidence shape fixed** (8 / 3×512 / 10 / 20) and **DEFLATE**-coded, reusing deps both sides already have | §6.6 |
| D30 | **Closed vocabularies** for severity and cause code, enforced by check constraint | §6.6, §7.4 |
| D31 | **Rule metric names align to the vitals names**, with the three current names accepted as aliases so pushed rules survive the rename | §6.5 |
| D32 | **VM is sized after Q3 measures.** The limit stays at 512 Mi; step 6 stops with the number and the four sizing options go to the project owner as an explicit decision — sizing off the same unmeasured ~2 KB figure Q3 exists to replace would repeat §1.5's error | §9.1 |
| D33 | **`/correlate` is removed outright**, not deprecated — no external consumers, and its only caller retires in the same step | §13 |
| D34 | **Retroactive reach is measured (Q11), not divided.** Measured at steady state through the production write path at three cap sizes, with the linearity between them as the evidence for the shipped cap: ~7 months, at the vitals shape it is only meaningful next to | §2.6, §9.1.1 |
| D35 | **Alert rate is measured (Q12), not estimated.** 0.2/device/day currently carries the evidence projection, the organization ceiling and a §14.2 revisit trigger; the pack does not roll past canary until the soak produces the real number | §6.6, §9.1.2 |
| D36 | **Assumed numbers that drive decisions become measured gates with the response escalated**, never a value picked from the estimate they exist to replace. Q3, Q11 and Q12 all follow this shape | §1.5, §9.1 |
| D37 | **Disk performance joins the vitals contract: `await_ms`, `await_ms.max`, `queue_depth`, worst device, Linux only.** `%util` is **excluded by decision** — the fleet is purely SSD/NVMe, where it pins at 100 % with headroom remaining | §6.2, §6.3.1 |
| D38 | **VMs and containers are both in scope, with different sources.** A VM measures normally and guest-observed latency is the correct reading; a containerized agent switches to its own cgroup and reports `unsupported` for latency rather than reporting host-wide I/O as its own | §6.3.2 |
| D39 | **The vitals cap stays 24 and its headroom is now spent.** The next vital re-opens D3 deliberately | §6.2 |
| D40 | **Tenancy is four levels — tenant → organization → site → device.** Isolation is enforced at the **tenant**; the hourly alert ceiling, the broadest incident grouping scope and rule-settings resolution all key on the **organization** (one customer). At the tenant, one customer's storm would consume every other customer's budget and unrelated incidents would merge | §3.2, §6.6, §7.3 |
