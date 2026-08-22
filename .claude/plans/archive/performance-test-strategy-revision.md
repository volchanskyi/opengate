# Performance-Test Strategy

**Every fact below was confirmed against the live cluster, the OCI limits and
billing APIs, or the code in this repo on 2026-08-20.** Anything not confirmed
is in §10 and is labelled as such — no figure appears in the body that was not
read off a system. Re-confirm §1.3 before acting on it: it describes a live
cluster, not a fixed configuration.

**Venue rule:** this plan uses no local machine resources. Every workload runs
on free-tier infrastructure — the OKE node, GitHub-hosted runners, or OCI
Object Storage. §5 states which family runs where and why.

## 1. Confirmed state

### 1.1 Defects in the current harness

Ten defects, each reproduced against the file and line named.

**D1 — Three gate rows have never evaluated.**
[`loadtest-summarize.sh`](../../../scripts/loadtest-summarize.sh) guards the relay
row on `.metrics.relay_msg_latency_ms.values?` — the k6 **v0.x** summary shape —
while [`load-test.yml:18`](../../../.github/workflows/load-test.yml) pins
`K6_VERSION: v1.6.1`, which writes statistics flat on the metric object. The
sibling `http` row in the same jq block already uses the shape-tolerant
`values()` helper; the relay row does not. The fixture in
[`loadtest-summarize.test.sh`](../../../scripts/tests/loadtest-summarize.test.sh)
encodes the v0.x shape, so the test stays green while the row never appears.
[`loadtest-regression-check.sh`](../../../scripts/loadtest-regression-check.sh)
gates `k6/relay-throughput/relay` at lines 66, 83 and 99 — three ceilings on a
series that never arrives.

**D2 — The register measurement is structurally zero.**
[`main.go:266`](../../../server/tests/loadtest/main.go) — `register()` returns
immediately after `codec.WriteFrame`, a write into a local QUIC send buffer, and
`main.go:217` stops the clock there. The device upsert happens later, in
[`conn.go`](../../../server/internal/agentapi/conn.go). The two ceilings on
`register` can never fire.

**D3 — The relay scenario measures an HTTP health check.**
[`relay-throughput.js:49`](../../../load/k6/scenarios/relay-throughput.js) fills
`relay_msg_latency_ms` from `health.timings.duration` — an unauthenticated
`GET /api/v1/health`. No WebSocket is opened; the file says so itself
("Without a real agent, we test the WebSocket upgrade path only"). Its threshold
comment (lines 18–25) calibrates 150 ms against a `kubectl port-forward` tunnel,
but the workflow runs k6 **in-cluster**
([`loadtest-k6-incluster.sh`](../../../scripts/loadtest-k6-incluster.sh),
`kubectl exec` into a pod on the target node). The same stale tunnel claim sits
in `loadtest-regression-check.sh:17-23`.

**D4 — A partial night is absorbed as data.**
[`loadtest-k6-run.sh:44`](../../../scripts/loadtest-k6-run.sh) discards the export
of an aborted k6 scenario — the right shape, and the only such rule that exists.
The QUIC half has none, and nothing records which scenarios produced rows. A
night where one half ran and the other produced nothing enters the trend
indistinguishable from a slow system.

**D5 — Load runs permanently pollute staging.**
[`session.js:25`](../../../load/k6/lib/session.js) registers a real user per
scenario per run through the open `/api/v1/auth/register`;
[`handlers_auth.go`](../../../server/internal/api/handlers_auth.go) places it in the
default tenant. No cleanup exists in `load-test.yml`. Measured in staging today:
**81 users, all 81 matching `%@test.local`, in 1 tenant** — every user in the
staging database is load-test residue.

**D6 — The harness does not emit the production telemetry shape.**
[`soak.go:31-37`](../../../server/tests/loadtest/soak.go) lists **13** dimensions;
[`vitals.go:33-52`](../../../server/internal/agentapi/vitals.go) allowlists **18**,
so five `.max` dims are never exercised. `soak.go:40` sends families
`cpu, memory, disk, network`; `vitals.go:56` names the contract as
`cpu, mem, disk, net, proc` — two wrong names, one missing. `vitalSeriesCap` is
**24** ([`vitals.go:64`](../../../server/internal/agentapi/vitals.go)).

**D7 — Missing instrumentation the strategy needs.** No database-pool gauges and
no agent-registration outcome metric exist in
[`metrics.go`](../../../server/internal/metrics/metrics.go). D2 cannot be fixed
without the latter.

**D8 — Observation is slower than the workload.** `gaugeRefreshInterval` is
**15 s** ([`background.go:188`](../../../server/cmd/meshserver/background.go)),
`scrape_interval` is **30 s**
([`vmagent-scrape.yaml:6`](../../../deploy/helm/monitoring/files/vmagent-scrape.yaml))
— up to 45 s of lag. A QUIC run finishes inside that window, so
`agents_connected` never leaves zero.

**D9 — The disposable stack cannot host agent load.**
[`docker-compose.test.yml:28-29`](../../../deploy/docker-compose.test.yml) publishes
only `8080:8080` — no QUIC 9090/udp. It declares no resource limits, runs no
metrics store, and sets `OPENGATE_TEST_MODE=true` at line 25, which **no Go
source reads** (`grep -r OPENGATE_TEST_MODE --include=*.go` returns nothing).

**D10 — Free-tier guards disagree.**
[`compute.rego:22-34`](../../../policy/terraform/compute.rego) denies only above
**4 processors / 24 GB**. Commit `a4637a17` (2026-06-28) moved the Terraform
`free_tier.tftest.hcl` guards in the `oke` and `compute` modules to **2 / 12**.
The two gates disagree, and the looser one would pass a plan the stricter one
rejects. *(Which figure matches Oracle's current grant is open — see §10.)*

**D11 — Minor.** `make load-test` ([`Makefile:299`](../../../Makefile)) runs
`api-baseline.js` and `relay-throughput.js` only, omitting
`concurrent-agents.js`, which CI does run. `buildTenantAgents` in
[`soak_telemetry.go`](../../../server/tests/loadtest/soak_telemetry.go) is not
called by `main.go`.

### 1.2 The free-tier envelope, read from the OCI limits API

| Resource | Free grant | In use | Headroom |
|---|---|---|---|
| Block storage (`total-free-storage-gb`) | **200 GB** | **200 GB** — 3 × 50 GB block + 1 × 50 GB boot | **0** |
| Object storage | 20 GB | **1.1 MB** (2 buckets, 23 objects) | ~20 GB |
| `vm-standard-e2-1-micro-count` | **2** | **0** | 2 |
| Ampere A1 (`standard-a1-core-count`) | service limit **250**, used **2** | 1 instance, 2 OCPU / 12 GB | see §10 |
| GitHub Actions | free for public repos | 50 workflows, all `ubuntu-latest` | on demand |

The block-storage line is the binding constraint and it is exact: the grant is
200 GB, and 200 GB is attached. **Every new instance needs a boot volume, and
OCI's minimum boot volume is 50 GB, so no instance of any shape can be added
free until 50 GB is released.** This — not processor-hours — is the confirmed
blocker on a second node.

`total-storage-gb` is 61440 and `standard-a1-core-count` is 250, so the tenancy
*can* provision past the grant. It would bill.

### 1.3 The cluster, measured

**One node**, `VM.Standard.A1.Flex`, 2 OCPU / 12 GB, ARM64 (Oracle Linux 8.10,
kernel 5.15 aarch64), Kubernetes v1.34.2, up 75 days.

| | Allocatable | Requested | Limits |
|---|---|---|---|
| Processor | 1830m | 1180m (64%) | 7860m (**429%**) |
| Memory | 9647964Ki (~9.2 GiB) | 1530Mi (16%) | 5198Mi (55%) |

Node root filesystem: **35.4 GiB total, 12.5 GiB free** (65% used).

**Every pod is Burstable or BestEffort — none is Guaranteed, production
included.** Nothing gives production priority in the eviction order.

| Workload | Requests | Limits |
|---|---|---|
| `opengate/opengate-server` | 100m / 128Mi | 1 / 512Mi |
| `opengate/opengate-postgres` | 100m / 128Mi | 1 / 512Mi |
| `opengate-staging/opengate-staging-server` | 50m / 96Mi | 500m / 384Mi |

**Storage:** three 50 GB block volumes and one 50 GB boot volume.

| Volume | Capacity | Used |
|---|---|---|
| Production Postgres | 48.9 GB | **66 MB** |
| VictoriaMetrics | 48.9 GB | **119 MB** |
| Loki | 48.9 GB | **5.2 GB** |

5.4 GB of real data occupies 146.7 GB of volumes. The monitoring chart requests
`loki.storage: 10Gi` ([`values.yaml:42`](../../../deploy/helm/monitoring/values.yaml))
but the live PVC is 50 GB — OCI enforces a 50 GB minimum, so volume **count**, not
volume size, is the only storage lever that exists.

Loki's volume is the only one holding real data, and most of it is not movable.
Measured inside the pod:

| Directory | Size | Would move to object storage |
|---|---|---|
| `chunks` | 5.1 GB | yes |
| `compactor` | 121.7 MB | **no** |
| `wal` | 10.6 MB | **no** |
| `tsdb-shipper-active` + `-cache` | ~360 KB | **no** |

The store is `object_store: filesystem`, schema v13, `retention_period: 336h`
(14 days) ([`loki-config.yml`](../../../deploy/helm/monitoring/files/loki-config.yml)).
The write-ahead log, compactor working directory and index cache stay on local
disk wherever chunks live, so moving chunks out does not free the volume — it
relocates Loki's scratch space onto the node root. §5 records why that trade is
declined.

**Staging Postgres has no volume.** Its StatefulSet declares no
`volumeClaimTemplates`; `data` is an `emptyDir`, so the staging database writes
into the 12.5 GiB of node root that production also depends on.

**VictoriaMetrics retention is `-retentionPeriod=30d`**, read from the live
StatefulSet args. Any trend comparison longer than 30 days has no data behind it.

**Cluster leftovers are cosmetic:** one `vm-probe-1774398` pod Completed 48 days
ago, and zero-replica ReplicaSets. Neither consumes processor or memory. No Chaos
Mesh or KEDA remnants exist.

### 1.4 The E2.1.Micro shape, from the OCI shape API

`VM.Standard.E2.1.Micro` — 1.0 OCPU, 1.0 GB memory, **0.48 Gbps** network,
2.0 GHz AMD EPYC 7551 (Naples), **x86_64**. Two are granted; zero exist.

Two properties decide its role: it is a different architecture from everything
the cluster runs, and at 1 GB with 0.48 Gbps it is a weaker generator than a
GitHub-hosted runner that costs nothing and needs no volume. See §5.

## 2. Decisions locked

| # | Decision | Source |
|---|---|---|
| 1 | One plan; credibility fixes interleaved with the new capability | user |
| 2 | Fixture anchors on the committed 500-device reference: **500**, **2,000**, and one lopsided customer holding most of the fleet | user |
| 3 | Degradation order: live control → commands → operator reads → live readings → history last | user |
| 4 | The unbounded `family` label is recorded as technical debt; **no code change in this plan** | user |
| 5 | The single API pass mark is tightened now | user |
| 6 | **No local machine resources.** Free-tier only: OKE node, GitHub-hosted runners, OCI Object Storage | user |

### On decision 5 — what tightening can and cannot buy

The api-baseline p95 trend holds one run above 100 ms, and it is the same night
that also carries the worst error rate and a missing QUIC row. Every candidate
threshold between 100 ms and 290 ms produces the identical verdict on the
retained trend: one breach. Tightening the number alone changes nothing.

The reason is that generator and target share two processors, so the
measurement's own spread exceeds any tightening available. The tightening lands
as asked — **200 → 100 ms** — and is paired with step 6, which gives the
generator and the target separate processor allocations. Without that pairing
the new number is cosmetic; with it, a real regression becomes visible.

**The pairing is a gate, not a preference:** the 100 ms mark becomes blocking
only after fresh runs show the spread has narrowed (§9).

## 3. Scope

### In scope

- Repairing the existing measurement: D1–D9, D11.
- A versioned profile format and a versioned evidence bundle.
- A stateful agent simulator (duration, ramp, heartbeat cadence, reconnect,
  backfill, tombstone, duplicate connection).
- Real relay coverage: k6 opens a browser-side WebSocket, the Go simulator joins
  the agent side.
- A fixture builder at the three sizes in decision 2.
- Normal/peak, spike, soak and a guarded breakpoint on staging at night.
- Volume and the scaling sweep on a GitHub-hosted runner.
- Measuring what a 2,000-device fixture actually weighs, so storage is a
  measurement rather than an assumption.
- Run-validity classification, so a partial night is recorded as invalid rather
  than absorbed as data.

### Out of scope

- Production as a target.
- Any workload on a local machine (decision 6).
- Absolute capacity claims from any environment available today.
- Multiple full server replicas — relay ownership is process-local.
- The `family`-label fix (decision 4 — debt register only).
- Buying infrastructure, or any change that moves a resource past its free grant.
- Reclaiming Loki's volume, or any change to the log store (§5).
- Agent host-machine resource cost, which needs real agents on real endpoints.

## 4. Domain model

### 4.1 Objects that persist

Only two things are serialised. Everything else is behaviour.

**Profile** — `load/profiles/<name>.yaml`. Versioned. Declares: schema version,
family, environment class, ordered phases (name, duration, operator arrival
rate, connected-agent count, session count), safety limits, and gates.

**Evidence bundle** — one directory per run, uploaded as a workflow artifact and
mirrored into Object Storage. Versioned JSON plus raw exports. Holds: source
revision, profile version, environment and generator fingerprints, fixture
counts, phase boundaries, offered vs achieved load, per-phase and per-journey
results, server/database/telemetry/node series, expected-rejection counts kept
apart from faults, the verdict, and cleanup proof.

### 4.2 Objects that are behaviour

| Object | Realised as |
|---|---|
| Operator journey | a k6 module exporting a function and a weight |
| Agent behaviour | a Go state machine in `server/tests/loadtest/` |
| Gate | a rule in the profile, evaluated against the bundle |
| Finding | a row in the bundle's verdict section |
| Observation | a timestamped sample in the bundle |

### 4.3 Relationships

1. A profile selects exactly one fixture size and one ordered phase list.
2. A phase activates journeys, agent behaviours and sessions at declared rates.
3. A run consumes one profile and one fixture and produces exactly one bundle.
4. Gates read the bundle; they never read live state and never alter the
   workload.
5. A failed gate becomes a finding. A saturated generator or a breached safety
   limit makes the run **invalid**, which is a third verdict, not a failure.

### 4.4 Ownership boundaries

| Component | Owns | Must not |
|---|---|---|
| k6 | operator HTTP, pauses, arrival rates, browser WebSockets, client-observed latency | open QUIC, create fixtures |
| Go simulator | QUIC, certificates, held-open connections, heartbeats, reconnects, telemetry, backfill, agent side of relay | drive operator HTTP |
| Fixture builder | tenants, customers, sites, users, enrolled devices, inventory, processes, audit, sessions, telemetry | run during a timed phase |
| Runner | profile resolution, target allowlist, phase sequencing, collection, verdict | generate load |
| Collector | observing | generate load |

## 5. Venues — free tier only

Three venues exist. None is a local machine.

| Venue | What it is | Confirmed capacity |
|---|---|---|
| **Staging on OKE** | `opengate-staging`, sharing one node with production | server capped 500m/384Mi; node 1830m allocatable, 1180m requested |
| **GitHub-hosted runner** | `ubuntu-latest`, the runner all 50 workflows already use; free for public repos | ephemeral, disposable, isolated from production |
| **OCI Object Storage** | 2 buckets in `axcrowpqlsio` | 20 GB grant, 1.1 MB used |

The runner replaces every workload the earlier draft sent to a local machine. It
is strictly better for that role: it is free, it is ephemeral so a volume test
leaves no residue, and it is the only venue where generator and target can be
given genuinely separate resource boundaries — which is unsatisfiable on a
single-node cluster and is the root cause behind decision 5.

**One caveat, honestly stated:** a runner is x86_64 and production is ARM64, so a
runner result is a *comparison* between fixture sizes or processor counts, never
an absolute capacity claim about production hardware. That is exactly what the
volume and scaling families need, and §3 already rules absolute claims out of
scope.

| Family | Venue | Why, measured |
|---|---|---|
| Normal / peak | **Staging at night** | server capped at 500m/384Mi; node has real headroom at 64% requested |
| Spike | **Staging at night** | same envelope, short burst |
| Soak / endurance | **Staging overnight** | the node is billed by existing, not by working, so an overnight soak adds no cost |
| Breakpoint / overload | **Staging, with guardrails** | saturating two processors throttles production's health probes; every pod is Burstable, so nothing protects production today |
| **Volume** | **GitHub runner** | staging Postgres is on `emptyDir` sharing 12.5 GiB of node root with production; the runner brings its own disk |
| Scaling sweep | **GitHub runner** | needs four or five processor points; the cluster offers two |

### Storage stays as it is

Free block storage is exactly full — 200 GB granted, 200 GB attached (§1.2) — so
the only way to give staging Postgres its own disk is to delete another volume,
and Loki's is the only candidate. That trade is **declined**, on four measured
grounds:

1. **It buys nothing the plan needs.** The volume family's only claim on staging
   was production-shaped ARM hardware, and §3 already rules absolute capacity
   claims out of scope. Volume testing compares the three fixture sizes against
   each other, which the runner does for free.
2. **It does not free the volume.** Loki's write-ahead log, compactor directory
   and index cache stay on local disk (§1.3). Deleting the PVC moves them to an
   `emptyDir` on the node root — adding pressure to the 12.5 GiB the move was
   meant to protect.
3. **The request churn is the wrong shape for the grant.** 18,206 chunk files
   against a 14-day retention is roughly 1,300 uploads a day, about 39,000 a
   month, before index writes, compaction, retention deletes or any query. That
   is the same order of magnitude as the free monthly request allowance (§10).
4. **It puts the only log store at risk for a credential.** The S3-compatible
   endpoint needs an OCI Customer Secret Key living in the cluster, and
   `schema_config` is keyed by date — new chunks would go to object storage while
   the existing 5.1 GB stayed filesystem-backed until physically copied. Grafana's
   Loki datasource and the `/observe` skill's LogQL workflows are the only path to
   a device's logs.

**Measure the fixture before spending storage on it.** Nobody has weighed a
2,000-device fixture. Production Postgres holds **66 MB** after 49 days with four
real agents, and the node root has ~9 GiB of margin before the kubelet evicts. If
the fixture lands well inside that margin, staging needs no volume and the
question closes. Step 9 measures it and reports; only a fixture that exceeds the
margin reopens storage as a topic.

**The two E2.1.Micro grants stay unclaimed** and are recorded as the remaining
free-tier reserve — available if a persistent off-cluster endpoint is ever needed,
at the cost of the next 50 GB released.

## 6. Quality bars and non-functional requirements

**Correctness of measurement.** Every gate row must be provably reachable: a
test asserts each declared series is actually produced by the extraction, using
the **pinned k6 version's** output shape. D1 exists because a fixture encoded the
wrong shape; the guard against a repeat is a fixture generated from the real
binary, not hand-written.

**Run validity.** A run is valid, failed, or **invalid**. Invalid covers a
saturated generator (below 20% processor headroom or above 90% memory), a
breached safety limit, or a missing mandatory bundle section. Invalid runs never
enter the trend. The discard rule in `loadtest-k6-run.sh:44` is the right shape
and gets extended to the QUIC half, which has none.

**Evidence durability.** VictoriaMetrics retains 30 days. The **bundle is
authoritative**; VictoriaMetrics is the dashboard. Bundles go to workflow
artifacts and to Object Storage, which has ~20 GB free and holds 1.1 MB today.

**Security.** No production hostname or namespace may be reachable, enforced by
an allowlist a supplied URL cannot override. Test users and devices live in their
own tenant, never the default one (D5). Certificate private keys must stop
leaving the cluster: `load-test.yml:158` copies `/data/ca.key` onto a
GitHub-hosted runner, and is replaced by enrollment-issued test certificates.

**Production safety.** Before any breakpoint run: production's server and
Postgres get Guaranteed quality of service (requests equal to limits) so they are
last in the eviction order, and the run carries a hard stop on node processor
saturation or memory pressure. Today both are Burstable at 100m/128Mi requests
against 1/512Mi limits, so this is a real change, not a formality.

**Free-tier safety.** No step provisions a resource past its grant. Block storage
must read 200 GB of 200 GB unchanged throughout — no step adds, deletes or
resizes a volume. Object storage must stay under 20 GB, which bounds bundle
retention.

**Maintainability.** One journey library; profiles vary rates and phases rather
than copying scripts. One profile schema, one bundle schema, both versioned.
Seeded, deterministic fixtures. No test-only endpoint or flag in the shipped
server.

**Repository constraints that bind the implementation.**

- New Go code under `server/tests/loadtest/` inherits the existing
  `dir:tests/loadtest` mutation shard — no new assignment needed. A new directory
  *would* need one ([`mutation-shards.sh`](../../../scripts/lib/mutation-shards.sh)).
- The Go coverage gate filters only `/testutil/` and `api/openapi_gen.go`, so
  `server/tests/loadtest/` counts toward the 80% threshold. Any expansion must be
  a thin `main` over a thick, tested library, or the gate goes red.
- Sonar classifies `server/tests/**` as test code, so there is no new-code
  coverage pressure from that side.
- New `scripts/tests/*.test.sh` files must be mode 100755.
- Test-first: every source change needs a failing test on the branch first.

## 7. Implementation steps

Ordered; each independently testable. Steps 1–9 need no product decision.

**1. Make the relay row reachable (D1).** Change the relay guard in
`loadtest-summarize.sh` to use the `values()` helper the sibling `http` row
already uses, which handles both shapes. Rewrite the fixture in
`loadtest-summarize.test.sh` to the **k6 v1.x** shape, keeping a v0.x case beside
it. Add a test asserting every series named in `loadtest-regression-check.sh` is
produced by the extraction — the guard that would have caught D1.

**2. Measure registration server-side (D2, D7).** Add an agent-registration
outcome-and-duration metric in `metrics.go`, recorded in `conn.go` where the
upsert completes. The harness reads it rather than timing its own buffer write.
Add database-pool gauges (open/active/idle/waiting) in the same change.

**3. Make the relay scenario open a relay (D3).** Replace the health-check
substitute in `relay-throughput.js` with a real WebSocket against the browser
side, paired with a Go simulator holding the agent side. Delete the stale
port-forward comments there and in `loadtest-regression-check.sh:17-23`.

**4. Record run validity (D4).** Extend the discard rule to the QUIC half. Emit
an explicit run-completeness record naming which scenarios produced rows and
which did not. A run missing any expected scenario is **invalid** and does not
enter the trend.

**5. Stop polluting staging (D5).** Create load-test users and devices in a
dedicated tenant, emit a cleanup manifest, verify zero residue after each run,
and delete the 81 accumulated orphan users. Replace the CA-key extraction in
`load-test.yml` with enrollment-issued test certificates.

**6. Separate generator from target, and tighten the mark (decision 5).** Give
the k6 pod and the QUIC pod their own processor and memory allocations distinct
from the server's. Then lower the api-baseline threshold 200 → 100 ms. The new
mark stays non-blocking until fresh runs show the spread has narrowed.

**7. Correct the emitted telemetry shape (D6).** Align `defaultMetricDimNames` to
the 18 allowlisted dims and `defaultFamilies` to `cpu, mem, disk, net, proc`.

**8. Build the runner-hosted performance stack (D9).** A performance compose file
executed by a GitHub-hosted runner: QUIC 9090/udp exposed, explicit processor and
memory limits on the server, the database and the generator **separately**, and a
small metrics store. Remove the dead `OPENGATE_TEST_MODE` variable. This is the
venue for the scaling sweep, and the only place generator and target are truly
isolated.

**9. Weigh the fixture (§5).** Build the 2,000-device fixture on the runner stack
and record its on-disk size, row counts and telemetry series count in a bundle.
Compare it against the node root's ~9 GiB of eviction margin. A fixture inside the
margin closes the storage question; one that exceeds it reopens it with a measured
number attached. **No storage changes until this number exists.**

**10. Profile format and evidence bundle.** The schema in §4.1: a thin `main`
over a tested library in `server/tests/loadtest/`, with schema-validation tests
and a completeness check that fails on a missing mandatory section.

**11. Fixture builder.** The three sizes from decision 2, seeded and
deterministic, built through public APIs on staging and by direct load on the
runner, outside every timed phase, with a cleanup manifest. Step 9's measurement
is the first thing it produces.

**12. Stateful agent simulator, then the families.** Duration, ramp, cadence,
seeded jitter, graceful stop; held-open connections and heartbeats; scheduled
telemetry cycles; reconnect, duplicate connection, backfill deferral, slow
response, tombstone. Then deliver in order: normal/peak → spike → guarded
breakpoint → soak (staging, overnight) → volume (GitHub runner) → scaling sweep
(GitHub runner).

**13. Housekeeping found on the way.** Resolve the `compute.rego` disagreement
with the Terraform guards (D10) once §10 item 2 is settled. Add
`concurrent-agents.js` to `make load-test` (D11). Remove the unused
`buildTenantAgents`. Delete the 48-day-old `vm-probe` pod and the zero-replica
ReplicaSets. Record the unbounded `family` label in
[`techdebt.md`](../../techdebt.md) with its pay-down trigger (decision 4).

## 8. The three dimensions

**Core logic.** Reusable journeys; stateful agent behaviours; phase sequencing;
expected-outcome classification; recovery calculation; resource-growth
evaluation; breakpoint search; volume and scaling comparison; deterministic
bundle generation.

**Scope boundaries.** k6 owns operator and browser behaviour. Go owns agent
behaviour. The fixture builder owns data and never runs inside a timed phase.
The collector observes and never generates. Staging at night carries normal,
peak, spike, soak and a guarded breakpoint. Volume and the scaling sweep run on a
GitHub-hosted runner. No workload runs on a local machine, and none changes the
log store or the storage layout.
Production is never a target. Horizontal scale stays out until distributed
session ownership exists.

**Definition of done.**

- Every gate row in `loadtest-regression-check.sh` has a test proving the
  extraction produces its series under the pinned k6 version.
- `register` reports a non-zero server-confirmed duration.
- The relay phase carries samples from an actual WebSocket.
- Any night missing a scenario is recorded as invalid and excluded from the
  trend.
- A run leaves zero residue: no orphan users, devices, tenants, pods or series.
- No certificate private key leaves the cluster.
- Every run emits a complete bundle; a missing mandatory section fails the run.
- Breakpoint reports name the last passing load, the first failing load, the
  limiting resource, and recovery.
- Soak reports carry memory, goroutine, open-file, pool, dead-row, series and
  storage trends.
- Volume reports compare performance and storage across the three fixture sizes.
- Three consecutive runs give the same verdict, or the environment stays
  non-gating.
- Production is provably unreachable from any profile.
- Block storage still reads 200 GB of 200 GB, unchanged, and the Loki volume is
  intact.
- The 2,000-device fixture's measured weight is recorded, and either sits inside
  the node root's eviction margin or is reported with the number that reopens the
  storage question.
- No step ran on a local machine.
- [`docs/infrastructure/Testing.md`](../../../docs/infrastructure/Testing.md),
  [`phases.md`](../../phases.md), [`techdebt.md`](../../techdebt.md) and a new
  **ADR-081** are current.

## 9. Verification

- **D1:** feed a real `--summary-export` from k6 v1.6.1 through
  `loadtest-summarize.sh` and assert a `phase="relay"` row appears; assert the
  same for every series named in the regression check.
- **D2:** run the harness against a server on the runner stack and assert the
  register duration is non-zero and matches the server-side metric.
- **D4:** delete one scenario's export mid-run and assert the run is classified
  invalid and pushes nothing.
- **D5:** run a full cycle and assert user, device and tenant counts return to
  their pre-run values; assert the staging user count reaches zero after the
  one-off cleanup.
- **Step 6:** compare the p95 spread across three runs before and after
  separating the generator, and confirm it narrows before the 100 ms mark
  becomes blocking.
- **Step 8:** record the runner's actual processor count, memory and disk in the
  first bundle it produces, so the scaling sweep's points are stated rather than
  assumed.
- **Step 9:** build the 2,000-device fixture, record its on-disk size and row
  counts, and compare against the node root's free space and the kubelet's 10%
  eviction threshold. Confirm `oci limits value list --service-name block-storage`
  still reads 200 GB of 200 GB and that the Loki PVC is untouched.
- **Whole:** `/precommit`, then a full nightly cycle producing a valid bundle.

## 10. Not confirmed

Everything above was read off a system. These seven were not, and none of them
blocks steps 1–11.

1. **GitHub-hosted runner capacity.** All 50 workflows use `ubuntu-latest` and
   the repository is public, both confirmed. Its processor count, memory and disk
   were not measured here — step 8 records them in the first bundle rather than
   assuming them, and the scaling sweep's points follow from that measurement.
2. **Whether the Always-Free A1 grant is 2/12 or 4/24.** `compute.rego` asserts
   4/24; commit `a4637a17` moved the Terraform guards to 2/12 on the basis that
   Oracle halved the grant. The OCI limits API exposes only the paid service
   limit (250 cores), not the free grant, so the API cannot settle it. Needs the
   OCI console. **Until settled, D10 stays open and no second node is
   provisioned** — §1.2's block-storage finding blocks one regardless.
3. **The OCI Always Free object-storage request allowance.** The limits API
   exposes only `bucket-count` and `storage-bytes` for object storage, not a
   monthly request ceiling, so the figure §5 weighs Loki's ~39,000 monthly uploads
   against is not confirmed here. The churn rate itself is measured; the ceiling
   is not. This affects only how strongly §5's third ground reads — grounds 1, 2
   and 4 decline the reclaim on their own.
4. **What a fifth 50 GB block volume would cost.** OCI list price puts it near a
   dollar a month; the rate was not verified. It is outside decision 6 either way,
   but it is the honest size of what declining the reclaim gives up.
5. **Peak objectives per journey** — device list, device detail, command
   acceptance, relay message. Today there is one API-wide number and a fleet of
   one device to calibrate it against.
6. **Recovery interval.** Assumed to be the repo's existing **≤ 60 s after the
   fault clears**
   ([Fault-Injection.md](../../../docs/infrastructure/Fault-Injection.md)) unless
   told otherwise.
7. **Soak schedule.** An overnight soak on staging is free, but a 24 h run
   crosses the login-token lifetime in
   [`main.go`](../../../server/cmd/meshserver/main.go), so renewal must be proven
   before a 24 h profile is scheduled.
