# Make the performance nightlies measure something, and measure it for long enough

Master plan. Follow-on to
[`performance-nightlies-repair.md`](archive/performance-nightlies-repair.md),
which repaired the four nightlies until they came back green. This one is about
what they say when they are green.

Everything in §1 was read off the running system on 2026-09-04 and 2026-09-05 —
the workflow run records, the uploaded evidence bundles, the k6 exports, the
live cluster, the live Oracle account, the live server's own exposition, and the
code. Nothing in it is inferred.

## 0. Progress

Updated 2026-09-09. Each workstream lands as one commit.

| WS | State | Where it is |
|---|---|---|
| WS0 | **Part-done** | Staging's 250 rung came back invalid for two reasons WS3 has now closed, so the ladder's answer is open again and the nightly itself is the first rung. The throwaway ladder is blocked on D38 |
| WS1 | **Done, then repaired** | `2292438c`, then the repair below. [ADR-100](../../docs/adr/ADR-100-a-bundle-field-is-a-reading-or-it-is-absent.md), [ADR-104](../../docs/adr/ADR-104-a-reading-names-whose-room-it-measures.md) |
| WS2 | **Done** | [ADR-101](../../docs/adr/ADR-101-one-measurement-one-limit-one-file.md) |
| WS3 | **Done** | [ADR-105](../../docs/adr/ADR-105-a-simulated-machine-is-one-machine-for-the-whole-run.md), [ADR-106](../../docs/adr/ADR-106-a-venue-lasts-as-long-as-the-run-it-holds.md), [ADR-107](../../docs/adr/ADR-107-a-family-runs-somewhere.md) |
| WS4 | Not started | Blocks the throwaway half of WS0 |
| WS5 | Not started | |
| WS6 | Not started | |

### What WS3 changed that later workstreams should know

- `-agents` sizes the **estate** a profile draws from, and a profile declaring a
  level above it is refused before the clock starts. It still does not decide
  how many machines connect — that is D38, and it is WS4's.
- A machine enrols once and reconnects after, so a burst is a burst of
  reconnections. The enrolment ceiling no longer caps a rung.
- The staging nightly walks `normal.yaml` rather than offering its fleet at
  once, against a server holding production's own 250m/384Mi, with a fleet of
  500. All four `workload_name` series went to `/2` for that, so the trend
  re-bases once and WS5's re-basing is a second bump on top.
- `normal.yaml`'s steady phase is 8 minutes, because the three browser-side
  scenarios take about six between them and the last of them needs a fleet that
  is not already draining.
- Every profile is named by a workflow and every family row in `Testing.md`
  names one that exists, both swept by
  [`loadtest-family-venue.test.sh`](../../scripts/tests/loadtest-family-venue.test.sh).
  A new profile fails the gauntlet until it is scheduled.
- A workflow matrix that assembles a profile path at run time is invisible to
  every sweep that reads workflows as text, so the shape matrix spells its paths
  out.

### What WS1's own readings got wrong, and what the repair was

Four nightlies came back red on 2026-09-07 and 2026-09-08. Three of them were
WS1's new readings failing in ways only a live run could show; the fourth was a
different defect the same look turned up. All four are repaired.

**Attainment was counted at the wrong event.** The fleet tallied an arrival when
a machine's *life ended*, and a profiled run holds every machine to the end of
the walk — so no machine's life ends inside a phase, every phase reported nought
arrivals against an offer it had met, and the 80% floor invalidated all five legs
of [run 34126361816](https://github.com/volchanskyi/opengate/actions/runs/34126361816)
on fleets that had all arrived. An arrival is now counted where it happens. A
machine that arrived and was later cut off is a fault rather than a failure to
arrive, so one machine no longer enters the attempted tally twice.

**A phase was as long as it said it was.** `FinishedAt` was
`StartedAt + declared duration` and the arrival rate divided by the same
declaration. The five legs above ran 15m42s against a profile declaring 3m30s,
and every phase in their bundles claimed the declaration. Both are now the
clock, so what the two halves compare is machines asked for against machines that
turned up.

**A machine asked to hold for nothing held for its whole budget.** A phase's
round trip is a machine with no hold and a thirty-second budget, and it waited
the budget out after registering: twenty of them are the twelve missing minutes
above. A flat run with no hold paid the same thirty seconds per machine.

**The generator's room was one look at the wrong box.** Written up as
[ADR-104](../../docs/adr/ADR-104-a-reading-names-whose-room-it-measures.md). It is
now bracketed around the load and read from the generator's own cgroup where
there is one — confirmed live on staging, where the load-test pod's 400
millicores and 384 MiB arrive as `cpu.max 40000 100000` and
`memory.max 402653184`. On the throwaway venue, where the generator shares the
box with the stack it drives on purpose, the figure is evidence and attainment is
the gate. This is what invalidated
[run 34108066636](https://github.com/volchanskyi/opengate/actions/runs/34108066636),
whose staging pod was reading production's load off the node.

**Two nightly steps could not read the status they branched on.**
[Run 34113731966](https://github.com/volchanskyi/opengate/actions/runs/34113731966)
failed with no message at all: `OUT="$(check)"; rc=$?` under GitHub's `bash -e`
ends the step at the assignment. Both sites are fixed and the shape is now swept
for — the rule is in
[`ci-cd-determinism.md`](../rules/ci-cd-determinism.md).

Underneath it was a real finding the shape had been hiding: the drill published
`netdrill_reconnect_attempt_seconds` of 62.945s against a floor of 35, for a
machine that lost its link, waited out the outage and came back on its first try
in seven seconds. The figure timed from the moment the machine *noticed* the loss
when no attempt had failed, which measures the outage — the luck the figure
exists to exclude and which `netdrill_reconnect_seconds` beside it already
carries. It is now absent for a machine that failed no attempt, and that night
reads as no regression.

**A shard's baseline suite was racing.**
[Run 34199421320](https://github.com/volchanskyi/opengate/actions/runs/34199421320)'s
`go-protocol-wire` shard failed before mutation started, taking the night's
canonical score row with it.
`TestControlStream_SendAfterStreamCloseFailsAndReconciles` waited for the device
row to go offline and then required a send to fail — but the connection is let go
and the row written several database round trips before the stream closes, and a
QUIC write into a still-open connection succeeds. The server now refuses a send
down a connection it has released, which is the window an API handler that
resolved the connection a moment earlier actually sits in: a technician's screen
said a restart reached a machine that had been gone for seconds.

Bundle schema is **3**.

### WS0, as far as it went

The two ladders are not equally available, and the reason is a new defect.

**The staging ladder runs today.** [`load-test.yml`](../../.github/workflows/load-test.yml)
passes no `-profile`, so the harness takes the `runFlat` path and `-agents` is
the fleet.

**The 250-machine rung answered on the first attempt, and the answer is that
staging cannot hold 250.** Run
[34069847528](https://github.com/volchanskyi/opengate/actions/runs/34069847528),
2026-09-07, came back `invalid` for two reasons the harness stated itself:

```
generator had 0.0% processor headroom (floor 20%), so the run measured the generator
enroll soak-t0-a62: enrollment refused with 429: {"error":"rate limit exceeded"}
```

Both are figures that could not have existed a day earlier, which is §5's own
test of whether WS1 worked. The headroom was a hardcoded 100 on every run ever
recorded and could not fire; it is now a reading, and the first reading it took
invalidated a run. The bundle carried
`"commit": "2292438cee297ef887739ab65aa4d031a738011b"` rather than `unknown`.

The refusals are D30 arriving on schedule: every machine a run starts enrols
afresh, so 250 arrivals are 250 enrolment requests against a ceiling the server
enforces on purpose. WS1's `ErrEnrollmentRefused` counts those apart from faults;
WS3's enrol-once is what stops them being produced at all.

**That headroom figure was the node's, and it is now the pod's.** The reading was
one instantaneous look at `/proc/loadavg`, which is not namespaced — so inside a
pod it described the node, production included, under a field named for the
generator. It is now bracketed around the load and read from the generator's own
cgroup, which the staging pod has:
[ADR-104](../../docs/adr/ADR-104-a-reading-names-whose-room-it-measures.md). The
0% that invalidated this run was a true reading of a real condition on that node —
the condition Decision 1 moves the generator off the cluster to escape — but it
was not a reading of the generator, so it is no longer the thing that decides.

So the ladder has its first rung's answer: **the largest fleet staging holds is
below 250**, on the pod as currently sized. The 500 and 1,000 rungs are not worth
dispatching until §2's Decision on the generator pod's reservation lands — the
pod reserves 100 millicores and 128 MB today and bursts to 400 and 384 MB — and
until D30's enrol-once removes the refusals, which otherwise cap every rung at
the enrolment ceiling rather than at anything about capacity.

**D38 — `-agents` sizes nothing in a profiled run, and the perf-stack is
profiled.** `runWorkload` hands the profile straight to the phase walk, whose
level is `phase.ConnectedAgents`; `-agents` survives only as the length of the
hostname plan, which the fleet indexes modulo. So
[`perf-stack.yml`](../../.github/workflows/perf-stack.yml)'s `agents` input, its
default of 500 and any dispatch value are read by nothing that decides how many
machines connect. Dispatching the throwaway ladder at 2,000 or 8,000 today
connects 500.

It is also the third term in D18's confusion. The 2026-09-05 volume bundle said
`devices: 2000` because the *fixture* planned that many, while `fixture-weight.json`
counted 500 because the *profile* connected that many, and `-agents=500` sat
between them meaning neither. WS1 split the first two apart; this is the third.

Decision 8 already calls for the volume legs to differ in machines actually
enrolled, so the fix belongs in WS4 — and until it lands, the throwaway ladder
cannot measure the thing it exists to measure.

### What WS1 and WS2 changed that later workstreams should know

- Bundle schema is **3**. The arrival pair split by side, so
  `offered_agent_arrivals_per_second` and `achieved_agent_arrivals_per_second`
  are the machine half and `offered_operator_arrivals_per_second` is the
  technician declaration, whose achieved half stays absent until WS5's k6
  projection fills it. `offered_sessions` travels the same way.
- `Fingerprint.CPUs` is fractional, and `Bundle.Validate()` refuses a memory
  figure below a mebibyte or a revision of `unknown`.
- An unmeasured generator invalidates a run. Anything that starts a run has to
  pass `-target-cpus`, `-target-memory-bytes` and `-commit` or the bundle is
  refused. What makes Decision 2's top rung self-policing is **attainment**, not
  headroom: on a venue where the generator shares the box with the stack it
  drives, only the fleet's own account of what arrived says whether the load was
  offered.
- Every number a night is judged by lives in `load/profiles/`, and
  [`loadtest-regression-check.sh`](../../scripts/loadtest-regression-check.sh)
  holds none. WS5's re-basing is therefore one pass through the profiles.
- A measurement carries at most one blocking gate per direction, and every
  measurement the summarizer emits is either limited or named in the profile's
  `ungated:` list with a reason. Both directions are held by
  [`loadtest-gate-series.test.sh`](../../scripts/tests/loadtest-gate-series.test.sh);
  a new emitted series fails it until somebody rules on it.
- [`loadtest-gate-venue.test.sh`](../../scripts/tests/loadtest-gate-venue.test.sh)
  reads which generators each workflow starts, so WS5 adding a k6 leg to the
  perf-stack makes `k6/...` limits legal there without the check being edited.
- The runner profiles gained machine-side limits and **nothing evaluates them
  yet** — the perf-stack has no summarizer and no publish job. That consumer is
  WS4's, and it is the same gap D12 names.

### Two findings the work turned up

- The gate-series fixture had never carried the journey metrics, so three
  limits on the technician journeys had never been exercised by anything.
- Three journey `latency_p50_ms` limits, inherited from a catch-all at 1000 ms
  while their tails are held to 300, 500 and 1000, can never fire. They are now
  declared unlimited with that as their reason.

## 1. Confirmed state

### 1.1 How long the nightlies actually apply load

[Load Tests run 33955940993](https://github.com/volchanskyi/opengate/actions/runs/33955940993),
12m29s end to end:

| Step | Elapsed |
|---|---|
| Cluster setup, lease, staged pods, k6 install, Go build | 1m20s |
| k6 api-baseline | 2m05s |
| k6 concurrent-agents | 2m02s |
| k6 relay-throughput | 1m03s |
| QUIC fleet, `LOADTEST_HOLD=8m`, overlapping the three above | 8m30s |
| Cleanup, summary, publish, gate | ~2m |

Each k6 scenario is `30s ramp → 1m at target → 30s down`, so **three minutes** of
the night are spent with load at its intended level. The eight-minute QUIC hold
is not load: 100 machines arrive in about 1.7 seconds and then send one heartbeat
every fifteen seconds, because the hold exists to give the relay scenario a
machine on the other end of its sessions.

[Performance Stack run 33962446522](https://github.com/volchanskyi/opengate/actions/runs/33962446522),
five jobs in parallel. The profile
([`scaling.yaml`](../../load/profiles/scaling.yaml)) is `30s ramp + 3m steady`;
the rest of each job is a compose build, a `go build` and a 500-machine fixture.

Total applied load across both nightlies: about **twelve minutes**.

### 1.2 The load is small, and the ceiling is the rate limiter

From the 2026-09-05 run's own k6 export:

| Scenario | Requests | Rate | Measured p95 | Threshold | Margin |
|---|---|---|---|---|---|
| api-baseline | 7,228 | 59 rps | 7.0 ms | 100 ms | 14× |
| concurrent-agents | — | 68 rps | 2.9 ms (p99 6.4) | 500 ms p99 | 78× |
| relay-throughput | — | 19 rps | 37.0 ms | 400 ms | 11× |

The ceiling is deliberate and documented in
[`api-baseline.js`](../../load/k6/scenarios/api-baseline.js): every virtual user
runs from one pod, so the whole scenario spends one per-address token bucket, and
the `sleep(1.5)` is sized to stay under it.

**D1 — the nightly's throughput is set by the rate limiter, not by the server**,
so it cannot detect a throughput regression at all, and its latency figures
describe a server that is idle.

### 1.3 Five of the seven families are unscheduled

[`load/profiles/`](../../load/profiles/) holds seven profiles. `scaling` and
`volume` are named by [`perf-stack.yml`](../../.github/workflows/perf-stack.yml).
`normal`, `peak`, `spike`, `soak` and `breakpoint` are named by nothing —
verified by grep across `.github/workflows/`, `scripts/` and `server/`.

[`load-test.yml`](../../.github/workflows/load-test.yml) passes no `-profile`, so
the nightly takes the `runFlat` path in
[`workload.go`](../../server/tests/loadtest/workload.go): every machine at once,
no phases, no safety ceilings, no gates. Its bundle records
`profile_name: "ad-hoc"`, `family: "normal"` — a default, not a run of
[`normal.yaml`](../../load/profiles/normal.yaml).

**D2 — no endurance test.** `soak.yaml` declares eight hours and runs never. The
leak class [`resource-conservation.md`](../rules/resource-conservation.md) was
written about walked staging 29 → 334 MiB over four nights before it was killed
against its limit. An eight-minute hold cannot see it; the conservation slope in
`conservation_test.go` covers the relay at integration tier and nothing covers a
whole night.

**D3 — no capacity test.** `breakpoint.yaml` runs never, so nothing establishes
where the system gives out.
[ADR-014](../../docs/adr/ADR-014-postgres-migration.md) names a ceiling of about
20,000 concurrent machines; the nightly connects 100, the most any run has ever
connected is 500, and no run has ever gone looking for the real number.

**D4 — no burst-recovery test.** `spike.yaml` runs never — the case where a
site's link comes back and two thousand machines reconnect at once.

**D5 — the docs describe the unscheduled families as running.**
[`Testing.md`](../../docs/infrastructure/Testing.md) §"The six families, and
where each runs" places normal/peak/spike on "staging at night", soak on
"staging overnight" and breakpoint on "staging, under guardrails".
[`docs-live-state.test.sh`](../../scripts/tests/docs-live-state.test.sh) cannot
catch this: its phrase list looks for past-state narration, and this is a
present-tense claim about something that does not happen.

### 1.4 Numbers that cannot fail

This is the same defect class
[`ci-cd-determinism.md`](../rules/ci-cd-determinism.md) and
[`resource-conservation.md`](../rules/resource-conservation.md) exist for: a
number that reports success without measuring. Every one below was re-confirmed
against the 2026-09-05 bundles.

**D6 — `achieved` is assigned from `offered`.**
[`sequence.go:88-89`](../../server/tests/loadtest/sequence.go#L88-L89) sets
`AchievedArrivalsPerSecond: phase.OperatorArrivalsPerSecond`.
[`bundle.go:101`](../../server/tests/loadtest/bundle.go#L101) states why the two
fields are separate — "a generator that could not produce the load reads exactly
like a system that could not absorb it" — and
[`validity.go:196`](../../server/tests/loadtest/validity.go#L196) invalidates a
phase below 80% attainment. The ratio is always exactly 1.0. The rule has never
fired and cannot.

**D7 — generator headroom is a literal.**
[`run_bundle.go:94`](../../server/tests/loadtest/run_bundle.go#L94) writes
`Headroom{CPUHeadroomPercent: 100, MemoryUsedPercent: 0}` unconditionally.
[`validity.go:153`](../../server/tests/loadtest/validity.go#L153) invalidates a
run below 20% headroom. Also dead.

**D8 — the scaling sweep does not record what it swept.**
[`run_bundle.go:78-79`](../../server/tests/loadtest/run_bundle.go#L78-L79)
hardcodes `CPUs: 1, MemoryBytes: 1`. All four bundles from the 2026-09-05 run —
the 0.5, 1, 2 and 4 processor legs — carry `"cpus": 1, "memory_bytes": 1`. The
generator's own `MemoryBytes` is the same literal.

**D9 — `Connected()` counts machines that have already left.**
[`quic_fleet.go:88`](../../server/tests/loadtest/quic_fleet.go#L88) removes a
machine from the running set only when it errored; one that completes its hold
normally stays counted for the life of the run. `achieved_connected_agents: 500`
is a count of machines started.

**D10 — phase latency is structurally empty.** `SampleLatency()` returns the
last *finished* machine's connect duration. With `-hold=3m` inside a 3m30s
profile, no machine finishes during the walk, so every phase in every perf-stack
bundle carries `latency_p50_ms`/`p95`/`p99` as `null`. The field is `omitempty`
and nothing complains.

**D11 — profile `gates:` are never evaluated.**
[`profile.go`](../../server/tests/loadtest/profile.go) validates their shape and
nothing reads them. Every ceiling in all seven profiles is decorative, including
the ones marked `blocking: true`. `Phase.Sessions`
([`profile.go:134`](../../server/tests/loadtest/profile.go#L134)) is validated
and drives nothing, so `scaling.yaml`'s `sessions: 5` runs zero sessions.

**D16 — three more phase fields are never assigned.** `runOnePhase` sets neither
`ErrorRate`, `Faults` nor `ExpectedRejections`, so all three are zero in every
phase of every profiled run. `safety.max_error_rate` and the error-rate
invalidation in `phaseReasons` are therefore dead for every profiled run; they
work only on the un-profiled nightly, where `connectPhase` fills `ErrorRate` in.

**D17 — the journeys section is always empty.** `Bundle.Journeys` is declared,
never written, and `null` in every bundle — while api-baseline already measures
three named journeys and publishes them into the trend.

**D21 — every staging bundle is unattributable.** The harness runs inside a pod,
which inherits no revision, so `commitFromEnvironment()` falls back to the string
`"unknown"` and `Bundle.Validate()` accepts it. The canonical trend rows carry
the real revision, because they are built on the runner; the bundle beside them
does not.

**D22 — the runner's measured shape is read by nothing, and one of the three
figures answers the wrong question.** `perf-stack.yml`'s "Record what this runner
actually is" step measures `PERF_RUNNER_CPUS=4`,
`PERF_RUNNER_MEMORY_KB=16373452` and `PERF_RUNNER_DISK_KB=151263856` into the job
environment. No later step, script or bundle field reads any of them.

The disk figure is also the wrong column. `df -k --output=size /` reports the
root filesystem's **total** size — 144 GiB — while what a run can actually use is
what is free after the runner image's preinstalled toolchain, which GitHub
documents as **14 GB**. The volume family's whole question is whether a fixture
fits, so the one measurement taken to answer it reports a number roughly ten
times the one that binds. `--output=avail` is the column, and nothing has ever
recorded it.

**D31 — the fixture's weight never enters the bundle.**
`Fingerprint.DiskBytes`, `FixtureCounts.DatabaseBytes` and
`FixtureCounts.TelemetrySeries` are declared with comments saying the volume
family's whole finding is about them, and nothing writes any of the three.
`perf-weigh-fixture.sh` produces the figure into a separate file that never
reaches the bundle.

### 1.5 Numbers that describe an intention rather than a fact

Worse than a literal, because they look measured.

**D18 — the fixture's machine count is the plan, not the fleet.**
`BuiltFixture.Counts()` reports `PlannedDevices`. The machines that actually
exist are the ones that enrolled, which is `-agents`. The 2026-09-05 nightly
bundle says `devices: 500` with `-agents=100`; the volume bundle says
`devices: 2000` while `fixture-weight.json`, measured from the database in the
same job, counts **500**.

**D19 — the fleet is never filed under its customers.** `FileDevices` is written,
tested, and called from nothing but its own test. The machines land wherever
enrolment puts them, which is no site at all. So the lopsided estate — one
customer holding most of the fleet, which is the entire reason that fixture
exists — has never been built, and `siteWithDevices()` in api-baseline finds no
site holding machines and silently falls back to the unfiltered fleet read on
every night.

**D20 — `large` and `lopsided` hold the same amount of data.** Both are
`referenceDevices * largeMultiple`. They differ only in distribution, and the
distribution is D19. A volume sweep over small/large/lopsided is therefore a
two-point sweep with a broken third leg.

### 1.6 What the sweeps produce, observed

All four legs of the 2026-09-05 scaling sweep returned identical phase results:
offered equals achieved, latency null, error rate 0, faults 0, target
fingerprint 1/1. The only figures that varied were connect p95 — 55 ms at 0.5
processors, 14 at 1, 16 at 2, 9 at 4. The night before read 55, 16, 22, 13.

**D12 — the sweep has no consumer.** `perf-stack.yml` has two jobs and neither
reads the other's output. There is no publish job, no trend, no regression gate,
and the uploads use `if-no-files-found: warn`. Four bundles are produced nightly
and nothing has ever compared them.

**D32 — one night cannot establish the sweep's shape.** 2026-09-04 was
non-monotonic and 2026-09-05 was monotonic, from the same code and the same
profile. A gate asserting "the curve moves with the variable" over one sample per
leg would have failed one night and passed the next. What both nights agree on is
that the curve is flat from one processor upwards, which is the real finding: the
load is too small to saturate even one processor, so the number of processors
cannot matter.

**D13 — the volume family has one volume point.** The job runs
[`volume.yaml`](../../load/profiles/volume.yaml) once, at `fixture: lopsided`,
and D18 means the fleet is 500 machines whatever the profile says.

### 1.7 Three methodology faults

**D14 — the workload model is closed-loop.** k6 `stages` and `constant-vus` hold
*users*, not *arrival rate*: each user waits for its own reply before sending
again, so when the server slows the offered load falls with it and latency
understates the damage. `operator_arrivals_per_second` in the profiles is the
open-loop model stated correctly and implemented nowhere. k6's
`constant-arrival-rate` / `ramping-arrival-rate` executors are the direct fix.

**D15 — the top of the sweep is contended.** At the `cpus: "4"` leg the server
alone claims the whole runner, alongside Postgres (1.0), VictoriaMetrics (0.5)
and the load generator running unlimited on the same host. The 2 and 4 points
measure oversubscription, not processors.

**D25 — the profile's operator numbers are offered by nothing, and cannot be
offered by the process that reads them.** `operator_arrivals_per_second` and
`sessions` describe technician load. The Go harness drives machines only; k6
drives technicians, and never sees the profile. In the perf-stack no k6 runs at
all, so `scaling.yaml`'s "hold the load constant and vary the resources" holds
*no* technician load constant. What its sweep actually varies processors against
is 500 machines arriving, which is the cheapest thing the server does — and that
is why D32's curve is flat.

### 1.8 Two gate systems, and one that cannot reach its own subject

**D23 — profile gates name series the process that would evaluate them cannot
see.** Twelve of the sixteen gate rows across the seven profiles name a
`k6/...` series. `Classify` runs inside the Go harness, whose bundle holds phases
and observations and no k6 row at all. The perf-stack runs no k6, so the single
gate on `scaling.yaml` and on `volume.yaml` names a series that structurally
cannot exist in the venue those profiles run in.

**D24 — two gate systems already exist and disagree.**
[`loadtest-regression-check.sh`](../../scripts/loadtest-regression-check.sh)
holds live absolute ceilings keyed by the same source/scenario/phase triple the
profiles' `gates:` name, with different numbers: `k6/api-baseline/http`
`latency_p95_ms` is 200 there and 100 in `normal.yaml`. Making the profile gates
live without reconciling the two gives one series two ceilings.

### 1.9 The venue, measured

Read from the live cluster and the live Oracle account on 2026-09-05.

**The Oracle allowance is spent.**

| | Allowance | Committed | Left |
|---|---|---|---|
| Processors | 1,500 processor-hours a month — two, continuously | one machine, 2 processors, always on = 1,440 | 60 processor-hours |
| Memory | 9,000 gigabyte-hours a month — 12 GB, continuously | 12 GB always on = 8,640 | 360 gigabyte-hours |
| Disk | 200 GB, boot disks included | three 50 GB volumes + one 50 GB boot volume | **nothing** |

The 60 spare processor-hours cannot be spent: a second machine needs a boot disk
of at least 47 GB and there is none to give. The three volumes hold 8.65 GB
between them — Oracle's minimum volume is 50 GB — so 200 GB is committed to hold
8.65 GB, and releasing any of it means moving Loki's logs onto the node root, the
trade [ADR-035](../../docs/adr/ADR-035-oke-free-tier-block-volume-remediation.md)
already refused.

**D26 — the cluster cannot hold what the profiles ask for, and never will.**
The node offers 1,830 millicores; 1,480 are reserved, leaving **350**, of which
the two generator pods already hold 200 — a ceiling
[`loadtest-workflow.test.sh`](../../scripts/tests/loadtest-workflow.test.sh)
already enforces. The staging server is capped at 500 millicores and 384 MiB.
`breakpoint.yaml` asks for 4,000 machines and `peak.yaml` and `spike.yaml` for
2,000; the most ever connected to staging is 100.

Memory is the opposite: 2,042 MiB of 9,426 MiB reserved, and the machine's actual
working set is 7.63 GiB of 11.4 with 3.77 GiB free. Live processor use is 25% of
two. So the binding constraint is *reservation*, not capacity — the machine is
three-quarters idle and nothing may reserve more than 350 millicores of it.

**D27 — a generator outside the cluster cannot reach staging's machines.**
Production binds 9090/udp and 4433/tcp on the single node and the firewall opens
exactly those two ports. Staging's machine listener has no host port and no
firewall rule, so it exists only inside the cluster. Staging's web entry point is
reachable through the shared load balancer, under the host name `localhost` with
no certificate.

**The throwaway machine, measured from the 2026-09-05 run's own log:** 4
processors and 16,373,452 KB of memory, on runner image `ubuntu24/20260831.293`.
Its root filesystem is 144 GiB, but that is the partition rather than the room:
GitHub documents **14 GB of free disk** for a standard runner, the rest being the
preinstalled toolchain, and no run has measured the free figure (D22). GitHub's
billing documentation states standard hosted runners, artifact storage and log
storage are free for public repositories with no minute limit. It consumes none
of the Oracle allowance.

Fourteen gigabytes is enough for everything this plan puts there, and the
arithmetic is worth writing down rather than assuming: the fixture weighing
measured 2.6 KB per machine, so an 8,000-machine volume leg is about 21 MB of
database; five hours of memory snapshots at five-minute intervals is about 15 MB;
the compose stack's images are the largest item at a few gigabytes. WS0 measures
the free figure rather than continuing to assume it.

**The server already publishes what a remote generator would need.**
`opengate_http_request_duration_seconds` is a live histogram labelled by method
and route — 408 buckets at rest — on the same internal port the harness already
reads. So a generator anywhere can produce the load while the server says how
long *it* took, and the path between them leaves the number. This is the doctrine
already applied to registration timing.

**The deepest memory technique is available on a throwaway machine and not on
staging.** The server publishes heap and goroutine profiles on that same port —
23 KB and 16 KB at rest — so snapshots taken one after another and compared are
free in either venue. Following what actually holds a leaked object needs a
debugger attached to the process or a crash dump, and the staging pod drops every
capability, runs as a non-root user and has a read-only filesystem. Enabling it
there means weakening a pod that shares a machine with production.

### 1.10 The schedule is a fiction

Measured across five workflows over six nights: every scheduled run starts **4.5
to 6.5 hours after its cron**.

| Workflow | Cron | Actual start, recent nights |
|---|---|---|
| mutation | 03:00 | 07:07–07:26 |
| e2e-cross-browser | 03:00 | 07:28–09:25 |
| benchmark | 04:00 | 08:15–08:49 |
| load-test | 05:00 | 08:41–09:16 |
| network-drill | 06:00 | 09:33 |
| perf-stack | 07:00 | 11:06–11:57 |

**D33 — the slots the comments describe do not exist.** The relative order holds
but the spacing compresses: the mutation matrix's real window is roughly
07:10–09:20, and the load test began *inside* it on two of the last three nights
(mutation 07:26→09:12 against load-test 09:09→09:17). Planning a new slot in
absolute time is planning against a fiction. The staging lease is the only real
serialisation between two runs on the cluster, and
[`perf-stack.test.sh`](../../scripts/tests/perf-stack.test.sh) currently pins the
07:00 cron with a comment restating the fiction.

### 1.11 Four limits that cap any long staging run

**D28 — thirty minutes is the ceiling today.** The pod holding the fleet is
created with `sleep 1800`, and `loadtest-quic-incluster.sh`'s collect timeout is
1,500 seconds.

**D34 — the staging claim expires at forty-five minutes and is never renewed.**
`staging-lease.sh` writes `renewTime` once at acquisition with
`leaseDurationSeconds: 2700`. Past that any waiter may take the namespace from
under a run still in progress.

**D35 — the credential the machines enrol with expires after one hour.**
`mintEnrollmentToken` asks for `expires_in_hours: 1`. Every machine a phase
starts calls the enrolment endpoint, so an eight-hour profile is refused from its
second hour onward.

**D36 — GitHub kills any job at six hours.** Confirmed against GitHub's own
limits page. An eight-hour hold cannot live inside one job.

### 1.12 Two more faults a long or bursty run would hit

**D29 — the machine stops proving its connection when `-hold` elapses.** After
`holdOpen` returns, `runAgentWithContext` parks on the run's context and writes
nothing more. The connection survives — the server keep-alives every 30 seconds
against a 90-second idle timeout, and a machine's online status follows the
connection rather than the heartbeat — but `ErrHeldPeerGone`, the only detector
of a severed fleet, goes blind for the remainder. In a profiled run that is every
minute past `-hold`.

**D30 — the harness re-enrols on every start.** `forAgent` mints a fresh identity
per call, so each machine a phase starts creates a new device row. A real machine
enrols once and reconnects with the certificate it already holds. Three
consequences: a burst of two thousand arrivals is two thousand enrolment
requests against a hundred-a-second ceiling; the fleet grows through the run, so
a long soak confounds itself with the volume dimension; and the event being
modelled — a site's link coming back — is not the event being run.

### 1.13 The soak, as written, would finish almost no work

The leak detector already in the bundle divides what the target failed to give
back by the number of finished operations between two readings. `soak.yaml` holds
500 machines and its `sessions: 3` drives nothing (D11), so its operations are
the 500 machines it started.

| Run | Finished operations | Wall clock |
|---|---|---|
| 2026-09-05 nightly, 100 machines | **1,285** | 8m 30s |
| `soak.yaml` as written | **500** | 8 hours |

**D37 — holding a connection is not work.** The ordinary night completes two and
a half times the operations of the eight-hour soak, in one fifty-sixth of the
time, because its relay scenario opens and closes about 1,200 sessions in a
minute. A soak that only holds connections is a poor leak test at any length. The
industry floor for an endurance run is four hours and the band for systems
holding long-lived connections is eight to twelve, but the length is secondary to
the churn.

### 1.14 Reliability

Sixteen of the last twenty Load Tests runs were red, though the last week is
mostly green. Seven of eleven Performance Stack runs were red. Those are holes in
a 14-day window that forms a baseline from three samples
([`loadtest-regression-check.sh`](../../scripts/loadtest-regression-check.sh),
`MIN_WINDOW_SAMPLES=3`).

Its tolerances — `LATENCY_REL_TOL=4.0`, `P99_REL_TOL=3.0`, `RPS_REL_TOL=0.65` —
are an accurate statement about a noisy measurement, and the noise is what §2
addresses. They are not to be tightened before the measurement is worth trusting.

## 2. Decisions taken

All settled with the user across five rounds of research. Nothing below is open.

### Decision 1 — the generator moves to a throwaway machine, and so do the four sized-up configurations

The generator is built to drive thousands of machines and technicians, and it
runs on a GitHub-hosted machine beside a stack that machine builds and destroys.

**Why.** The two sides of a load test want opposite sizes: the generator has to
be big enough to *produce* the load, and the server under test has to be exactly
as small as the real one so the answer is about the real one. On the Oracle
cluster they compete for the same 350 unreserved millicores, so every number
staging has ever produced is about that competition. On a throwaway machine the
server takes its 250 millicores and the generator takes the other 3.75
processors and 15 GB.

**Pros.** Free, and consumes none of the Oracle allowance, which §1.9 shows is
fully spent and cannot grow. Removes the hazard of starving production's health
probes on the shared machine. Makes the deepest memory technique available,
because the run owns the whole box and destroys it.

**Cons.** The processors are Intel or AMD and production's are Ampere, so a
capacity figure from it is a comparison and never an absolute claim about
production. Stated in the run's own record, exactly as the two families already
on that machine state it.

**What a technician sees.** Today, asked "we have 3,000 machines across 40 sites
— will one of your servers hold them?", the honest answer is that nothing has
ever connected more than 500 to anything and the 20,000 on paper has never been
tested. This produces "a server given production's allowance held N machines, the
first thing to run out was X, and it recovered on its own afterwards."

### Decision 2 — the sized-up ladder is four configurations, plus a fifth on the real hardware

| Leg | Processor | Memory | Venue | Question it answers |
|---|---|---|---|---|
| baseline | 250m | 384 MB | throwaway | what production's own shape holds |
| half | 500m | 768 MB | throwaway | what half a processor would buy |
| one | 1000m | 1.5 GB | throwaway | what a whole processor would buy |
| two | 2000m | 3 GB | throwaway | where the curve stops rising |
| ampere | 250m | 384 MB | **staging** | the same shape on the hardware we actually run |

The ampere leg uses an in-cluster generator sized to the most the node's
reservations allow. It is what makes the other four interpretable: the baseline
runs on both, so the two readings at the shared point give a measured conversion
between the two processor families, and every other leg can be reported raw and
converted. That conversion is a single-point calibration and is labelled as an
estimate, not a law.

**Pros.** Every rung is a decision somebody could take. It shows where the free
tier stops helping — §1.9 puts that at roughly 500 millicores, because 350 is all
the node has left to reserve, ever.

**Cons.** Five legs a night. The top rung is contended on a four-processor
machine (server 2.0 + database 1.0 + metrics 0.5 leaves 0.5 for the generator),
and that is deliberate: with D7 repaired, the top rung reports its own starved
generator and is marked invalid rather than answering wrongly. The sweep says
where it stopped being able to answer.

**What a technician sees.** "Contoso is growing from 800 machines to 2,500. Do we
need a bigger server?" becomes a table rather than a guess, with the free-tier
edge marked on it.

### Decision 3 — staging keeps the everyday run, and becomes production-shaped

Staging carries `normal.yaml` nightly — the everyday shape on real hardware — and
the ampere leg of the sweep. Its server moves to requests equal to limits at 250
millicores and 384 MB, matching production exactly.

**Pros.** Staging becomes a reality check rather than an approximation, and
reserving what it is capped at puts it last in the eviction order.

**Cons.** It consumes 200 of the node's 350 spare millicores, so the generator
pods' reservations come down from 200 to 150 and they burst instead. Reservation
governs admission; the machine is 25% busy, so bursting is what the cluster
already relies on — its processor limits are 347% oversubscribed. Production
reserves exactly what it is capped at, so it keeps its quarter-processor whatever
a generator does.

### Decision 4 — the request limit is spread by presenting one address per technician, and the trust rule is narrowed in the same work

**Proven live on staging on 2026-09-05**, from a separate pod over the same
in-cluster name the generator uses:

| Sent | Answered | Refused |
|---|---|---|
| 400 requests, one claimed address | 252 | 148 |
| 40 requests, a second claimed address, immediately after | 40 | 0 |

**Pros.** No product code changes for a test's benefit, which
[`test-value.md`](../rules/test-value.md) requires. It exercises the real limiter
at full strength rather than switching it off. It is more faithful — real
technicians do arrive from many addresses. Nothing else in the server reads that
address: the request log records method, path, status, duration and a correlation
id, and no metric or audit entry is labelled by it, so a synthetic address
pollutes nothing.

**Cons.** It leans on a trust rule broader than it should be — *anything* inside
the cluster is believed, not only the entry point at the edge. So the same work
narrows it to the entry point, named by its service rather than a pinned address,
and extends `TestExtractIP`. The two move together, so nobody tightens the rule
later and breaks the nightly on the same day.

The alternative — counting authenticated requests per account — is the better
product design and does not solve this: all twenty simulated technicians share
one sign-in, so the bucket would move from one address to one account. It is
recorded as its own work in §6.

**What a technician sees.** Northwind IT's forty technicians share one office
connection. Steady browsing is nowhere near the limit — the pages refresh every
15 to 60 seconds, so forty idling technicians are about three requests a second.
But a bad patch goes out at 09:05, everyone opens a machine's page at once, six
requests each, two hundred and forty in a second or two against a saved-up
allowance of two hundred. Somewhere past the thirtieth technician the page comes
back empty with nothing explaining why. That is the entry in §6.

### Decision 5 — the soak runs on the throwaway machine, for five hours, with churn

**Pros.** Five hours fits inside one job with an hour to spare, so none of D28,
D34, D35 or D36 has to be defeated first. Snapshots taken one after another are
free, and attaching a debugger to find what is actually holding memory is a
one-line addition on a machine that is destroyed. The server's memory ceiling is
pinned to production's real 384 MB — already a variable in the stack — so the
eviction case is reproduced rather than lost.

**Cons.** Not the eight hours previously agreed, and not the shared machine. Both
are answered by churn: with machines leaving and rejoining and sessions opening
and closing, five hours finishes tens of thousands of operations against the 500
an eight-hour idle hold would (§1.13). If five hours of churn does not surface a
leak the eight-hour version would, that is recorded and the longer run is built.

**What a technician sees.** The defect this exists for stranded two goroutines
per finished session, forever. Nobody noticed until staging was killed against
its memory limit four nights later, and every liveness number the server
published read healthy throughout. With churn, the same defect shows as a line
that will not flatten within the first hour of one run, and the goroutine dump
names the exact line it is stuck on.

### Decision 6 — the technician load moves to arrival-rate pacing

api-baseline and concurrent-agents move to k6's arrival-rate executors, so a slow
server shows as a queue rather than as a quieter test. relay-throughput stays
user-based — one session per iteration is the unit. `dropped_iterations` becomes
a gated series: it is the signal that the generator could not keep the rate, and
without gating it the change silently degrades back into the closed loop.

### Decision 7 — the profile is projected into k6, and gates are evaluated where both halves exist

`operator_arrivals_per_second` and `sessions` are technician-side numbers, so k6
is handed them from the profile rather than restating them in its own `options`.
That is what makes the profile the single source of truth
[ADR-082](../../docs/adr/ADR-082-load-runs-measure-the-system-or-say-they-did-not.md)
says it is, and it is what lets the perf-stack hold a real technician load
constant while it varies the resources (D25).

Gates are evaluated in the publish step, over the canonical rows, where the k6
half and the QUIC half have been joined — not inside the harness, which cannot
see a k6 row (D23). `loadtest-regression-check.sh`'s absolute ceilings move into
the profiles so one series has one home (D24).

### Decision 8 — the volume family sweeps machines, not fixture names

D18 and D20 mean the fixture name does not control how much data is there;
`-agents` does. The volume legs therefore differ in machines actually enrolled —
500, 2,000, 8,000 — and `FileDevices` is wired so the lopsided distribution
exists at all.

### Decision 9 — every load-shape change bumps the workload name

[ADR-092](../../docs/adr/ADR-092-a-trend-series-carries-the-workload-that-produced-it.md)
exists because four scenario changes silently poisoned a fourteen-day window.
This plan changes the shape of three scenarios. Each takes a new version in
`workload_name`, in the same commit, so it compares against itself.

## 3. Scope

### In scope

- D1–D37.
- Every dead measurement made real, each with a test that fails when it goes
  dead again.
- The unscheduled families given a schedule, or deleted.
- An aggregation job for the sized-up sweep and the volume sweep.
- Steady-state percentiles taken over the steady window rather than the whole
  run.
- Docs, ADR, ledger, register.

### Out of scope

- Tightening the regression tolerances. They are calibrated against the current
  noise; re-calibrating is a follow-on once WS1–WS6 have produced a fortnight of
  runs.
- Production as a load target.
- Counting authenticated requests per account (§6 entry).
- A tenant-creation API (the existing debt entry and its trigger stand).
- Releasing block storage or adding a second node. §1.9 settles that neither is
  available.

## 4. Workstreams

Each is a micro-plan's worth of work and is written to be handed out separately.
Order matters: WS1 comes first because every later verdict rests on it, and WS0
comes before WS1 because it is what sizes everything else.

### WS0 — Measure the two venues before anything is scheduled

Three dispatched runs, no product changes.

1. **The in-cluster generator's real ceiling.** Raise the generator pod to
   reserve 150 millicores and 512 MB with a burst limit of 1,500 and 2 GB, and
   run staging at 250, 500 and 1,000 machines, watching both pods and the node.
   Output: the largest fleet staging can hold, which sizes the ampere leg.
2. **The throwaway machine's real ceiling.** The same at 500, 2,000 and 8,000
   machines against the 250-millicore stack. Output: the generator's cost per
   machine, which sizes every profile.
3. **The cost of churn.** One thirty-minute run with machines leaving and
   rejoining, to measure operations completed per minute. Output: the churn rate
   the soak needs to reach a useful denominator in five hours.
4. **The throwaway machine's free disk.** `df --output=avail`, before and after
   the stack is built, recorded into the bundle. Output: the room a volume leg
   and a soak's snapshots actually have, which D22 shows nobody has measured.

**Guard:** the three figures are written into the profiles as comments beside the
numbers they justify, and a shell test fails when a profile asks for more
machines than the recorded ceiling for its venue.

### WS1 — Make the dead measurements real (D6–D10, D16–D18, D21, D22, D31)

The harness measures what it claims, or says it could not.

1. **Attainment (D6).** The sequencer records the arrival rate actually achieved,
   read from what k6 reports for the phase rather than restated from the profile.
2. **Headroom (D7).** Read the generator's own processor and memory use across
   the run, the way `LocalNodeReading` already does for the node. A reading that
   could not be taken reports *not measured* and invalidates. This is what makes
   the sweep's top rung self-policing (Decision 2).
3. **Target fingerprint (D8).** The processor and memory limits of the system
   under test are passed in and recorded, for both sides. `perf-stack.yml` already
   knows them — they are the matrix value.
4. **Connected count (D9).** `forgetLocked` on every completion, and the server's
   own `opengate_agents_connected` recorded beside the generator's count so a
   disagreement between the two is visible rather than assumed away.
5. **Phase latency (D10).** A canary machine connects during each phase and its
   round trip is the phase's figure. The control stream has no reply to a
   heartbeat, so a fresh connect-handshake-register is the only live round trip
   the machine side has.
6. **Phase outcomes (D16).** `ErrorRate`, `Faults` and `ExpectedRejections` are
   assigned from what the phase actually saw.
7. **Journeys (D17).** The three journeys api-baseline already measures are
   carried into the bundle.
8. **Fixture counts (D18).** The bundle reports machines that enrolled, not
   machines that were planned; the plan travels beside it under its own name.
9. **Revision (D21).** The run's revision is passed into the pod rather than
   defaulted to `"unknown"`, and `Bundle.Validate()` refuses that string.
10. **Runner shape (D22) and fixture weight (D31).** Both are recorded into the
    bundle, which is the only place a later reader can find them.

**Guard:** a test that `AchievedArrivalsPerSecond != Offered` for a fleet
deliberately held below its offered rate; a test that a bundle whose target
fingerprint is the hardcoded `1/1` fails `Bundle.Validate()`; a test that
`Connected()` falls when a machine completes; a test that a bundle carrying
`"unknown"` as its revision fails validation.

### WS2 — Evaluate the gates where both halves exist (D11, D23, D24)

`Profile.Gates` is read in the publish step against the canonical rows and
produces `GateBreaches`, which `Classify` already consumes.
`loadtest-regression-check.sh`'s absolute ceilings move into the profiles.
`Phase.Sessions` drives concurrent sessions through Decision 7's projection, or
comes out of the schema.

**Guard:** a test that a profile with a deliberately breached blocking gate
classifies `failed` and one whose gates are all clear classifies `valid`; and an
extension of
[`loadtest-gate-series.test.sh`](../../scripts/tests/loadtest-gate-series.test.sh)
that refuses a profile naming a series its own venue cannot produce.

### WS3 — Repair the venue, then schedule the families (D2–D5, D26–D30, D33–D37)

1. **The four staging limits.** The pod's lifetime becomes the run's; the claim
   is renewed on an interval; the enrolment credential's life covers the run; and
   a held machine keeps proving its connection for as long as it is held (D29).
2. **Enrol once, reconnect after (D30).** A machine's identity is minted once and
   reused, so a burst is a burst of reconnections rather than of enrolments, the
   fleet stops growing through a long run, and the modelled event is the real one.
3. **Staging becomes production-shaped** per Decision 3, with the arithmetic
   written down.
4. **The schedule is relative, not absolute (D33).** Every workflow comment
   describing a wall-clock slot is corrected to describe an order, the staging
   lease is named as the real serialisation, and the waits are sized for a run
   that starts hours late.
5. **The families get their venues** per Decisions 1, 2 and 5. Each gets a bundle
   upload with `if-no-files-found: error`. `Testing.md`'s family table is
   rewritten to describe what runs.

**Guard:** a shell test that every profile in `load/profiles/` is named by some
workflow, and that every family row in `Testing.md` names a workflow that exists.
This closes D5's blind spot — a present-tense claim about something unscheduled —
which the live-state gate structurally cannot see.

### WS4 — Give the sweeps a consumer (D12, D13, D15, D19, D20, D32)

1. An aggregation job with `needs:` on the sweep matrix that downloads all legs,
   publishes the curve, and fails when **every leg is identical** — the condition
   that actually occurred. It does not assert monotonicity: D32 shows one night
   cannot establish a shape.
2. The volume job becomes a matrix over machines actually enrolled per Decision
   8, with `FileDevices` wired so the lopsided distribution exists (D19, D20).
3. The generator's own share is declared in the stack, so contention at the top
   rung is reported by WS1's headroom reading rather than inferred (D15).
4. Uploads move to `if-no-files-found: error` now that something downstream
   reads them.

### WS5 — Load worth the name (D1, D14, D25)

1. Per Decision 4, the generator presents one address per simulated technician
   and the trust rule is narrowed to the entry point at the edge, in the same
   commit, with `TestExtractIP` extended both ways.
   `loadtest-rate-budget.test.sh` is re-pointed at whatever the new binding
   constraint is.
2. Per Decision 6, api-baseline and concurrent-agents move to arrival-rate
   executors and `dropped_iterations` becomes a gated series.
3. Per Decision 7, the profile's technician numbers are projected into k6, and
   the perf-stack gains a k6 leg so its families hold a real technician load
   constant.
4. Steady phases lengthen to 3–5 minutes and percentiles are taken over the
   steady window, not over the ramps as well.
5. Thresholds are re-based on what the widened load actually produces, and every
   changed scenario takes a new `workload_name` version per Decision 9.

### WS6 — See inside the leak (D37)

1. The soak takes a heap profile and a goroutine profile on an interval and keeps
   every one. Both are already published on the port the harness reads; a
   goroutine dump is 16 KB and a heap profile 23 KB at rest, so five hours at
   five-minute intervals is about 15 MB.
2. The run reports the difference between consecutive snapshots, so the finding
   is "this grew, at this line" rather than "something grew".
3. For a stuck-goroutine leak the dump *is* the answer: it prints every
   goroutine's full stack, so it names the file and line and how many are stuck
   there. Against the defect that cost 7,148 goroutines it would have printed the
   offending line 7,148 times.
4. For a held-object leak Go's own profiler says where an object was born, not
   what holds it. A debugger-based reference walk is added on the throwaway
   machine, where the run owns the box and destroys it.

## 5. Verification

- `/precommit` green.
- The scheduled runs are the verdict, as in the prior plan. A green night after
  WS1 must show at least one figure that could not have been produced before: a
  measured headroom below 100, a target fingerprint that is not 1/1, an
  attainment that is not exactly 1.0, a phase error rate that is not zero, or a
  bundle whose revision is a real one.
- The sweep aggregation must fail on a deliberately identical set of legs.
- Break the product and watch a run go red, per
  [`test-value.md`](../rules/test-value.md): pin the server at 125 millicores and
  require the sweep to report the curve moving; hold a goroutine per session and
  require the conservation slope to red and the goroutine dump to name the line.
- The two readings at the shared 250m/384MB point — throwaway and ampere — are
  published together, so the conversion between the two processor families is a
  measured figure rather than a caveat.

## 6. Register entries this plan creates

- **The trust rule is narrower than it was, and the load test depends on it.**
  The generator presents one address per technician and the server believes it
  only from the entry point at the edge. Pay-down trigger: none — this is the
  end state. Recorded so the dependency is visible to whoever touches either.
- **Authenticated requests are still counted per address.** A customer whose
  technicians share one office connection shares one allowance, and a
  simultaneous rush can refuse the last of them with nothing on screen saying
  why. Pay-down trigger: any work on the request path, or the first customer
  report of a refused page during a busy moment.
- **The eight-hour soak is not built.** Five hours with churn finishes far more
  work than eight idle ones, and the venue limits that block a longer run on
  staging are catalogued in §1.11. Pay-down trigger: a leak that five hours of
  churn does not surface.
- **Staging's machine listener is unreachable from outside.** One node, one
  9090/udp. Pay-down trigger: a second node, which §1.9 shows is blocked by block
  storage.

## 7. Reviewer checklist

- [ ] No number in a bundle is a literal. Every field either comes from a
      reading or is absent.
- [ ] No number in a bundle describes an intention. A count of machines is a
      count of machines that exist.
- [ ] Every validity rule in `validity.go` has a test that makes it fire.
- [ ] Every profile in `load/profiles/` is named by a workflow, or is deleted.
- [ ] Every gate names a series its own venue can produce.
- [ ] One series has one ceiling, in one file.
- [ ] `Testing.md`'s family table matches the workflow files.
- [ ] Every changed scenario carries a new `workload_name` version.
- [ ] Every artifact something downstream reads is `if-no-files-found: error`.
- [ ] No workflow comment describes a wall-clock slot as if it were kept.
- [ ] The regression tolerances are unchanged in this work.
- [ ] Docs, ADR, `decisions.md` row, `phases.md` row, `techdebt.md` entries for
      everything in §6.
- [ ] This plan archived in the commit that lands its final workstream.
