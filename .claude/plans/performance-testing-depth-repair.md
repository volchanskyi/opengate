# Make the performance nightlies measure something, and measure it for long enough

Master plan. Follow-on to
[`performance-nightlies-repair.md`](archive/performance-nightlies-repair.md),
which repaired the four nightlies until they came back green. This one is about
what they say when they are green.

Everything in §1 was read off the running system on 2026-09-04 — the workflow
run records, the uploaded evidence bundles, the k6 exports and the code — not
inferred.

## 1. Confirmed state

### 1.1 How long the nightlies actually apply load

[Load Tests run 33903674959](https://github.com/volchanskyi/opengate/actions/runs/33903674959),
12m25s end to end:

| Step | Elapsed |
|---|---|
| Cluster setup, lease, staged pods, k6 install, Go build | 1m45s |
| k6 api-baseline | 2m04s |
| k6 concurrent-agents | 2m03s |
| k6 relay-throughput | 1m03s |
| QUIC fleet, `LOADTEST_HOLD=8m`, overlapping the three above | 8m35s |
| Cleanup, summary, publish, gate | ~2m |

Each k6 scenario is `30s ramp → 1m at target → 30s down`, so **three minutes** of
the night are spent with load at its intended level. The eight-minute QUIC hold
is not load: 100 agents arrive in about a second and then send one heartbeat
every fifteen seconds, because the hold exists to give the relay scenario a
machine on the other end of its sessions.

[Performance Stack run 33870350061](https://github.com/volchanskyi/opengate/actions/runs/33870350061),
8m38s, five jobs in parallel. Compose build 1m06s–1m41s; the profile step
6m32s–6m42s, of which the profile itself
([`scaling.yaml`](../../load/profiles/scaling.yaml)) is `30s ramp + 3m steady`.
The remaining ~3 minutes is `go build` plus building a 500-device fixture.

Total applied load across both nightlies: about **twelve minutes**.

### 1.2 The load is small, and the ceiling is the rate limiter

From the run's own k6 export:

| Scenario | Requests | Rate | Measured p95 | Threshold | Margin |
|---|---|---|---|---|---|
| api-baseline | 7,240 | 59 rps | 5.9 ms | 100 ms | 17× |
| concurrent-agents | 8,165 | 68 rps | 2.3 ms (p99 5.1) | 500 ms p99 | 100× |
| relay-throughput | 1,187 | 19 rps | 26.8 ms | 400 ms | 15× |

The ceiling is deliberate and documented in
[`api-baseline.js`](../../load/k6/scenarios/api-baseline.js): every virtual user
runs from one pod, so the whole scenario spends one per-IP token bucket, and the
`sleep(1.5)` is sized to stay under it.

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
written about walked staging 29 → 334 MiB over four nights before an OOM kill.
An eight-minute hold cannot see it; the conservation slope in
`conservation_test.go` covers the relay at integration tier and nothing covers a
whole night.

**D3 — no capacity test.** `breakpoint.yaml` runs never, so nothing establishes
where the system gives out.
[ADR-014](../../docs/adr/ADR-014-postgres-migration.md) names a ceiling of about
20,000 concurrent agents; the nightly connects 100, and no run has ever gone
looking for the real number.

**D4 — no burst-recovery test.** `spike.yaml` runs never — the case where a
site's link comes back and two thousand machines reconnect at once.

**D5 — the docs describe the unscheduled families as running.**
[`Testing.md`](../../docs/infrastructure/Testing.md) §"The six families, and
where each runs" places normal/peak/spike on "staging at night", soak on
"staging overnight" and breakpoint on "staging, under guardrails".
[`docs-live-state.test.sh`](../../scripts/tests/docs-live-state.test.sh) cannot
catch this: its phrase list looks for past-state narration, and this is a
present-tense claim about something that does not happen.

### 1.4 Six measurements that cannot fail

This is the same defect class
[`ci-cd-determinism.md`](../rules/ci-cd-determinism.md) and
[`resource-conservation.md`](../rules/resource-conservation.md) exist for: a
number that reports success without measuring.

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
hardcodes `CPUs: 1, MemoryBytes: 1`. All four bundles from the 2026-09-04 run —
the 0.5, 1, 2 and 4 processor legs — carry `"cpus": 1`. `perf-stack.yml`'s own
comment says a comparison between processor counts is only a comparison if the
counts are recorded beside the numbers they produced.

**D9 — `Connected()` counts machines that have already left.**
[`quic_fleet.go:88`](../../server/tests/loadtest/quic_fleet.go#L88) removes an
agent from the running set only when it errored; one that completes its hold
normally stays counted for the life of the run. `achieved_connected_agents: 500`
is a count of machines started, not connected.

**D10 — phase latency is structurally empty.** `SampleLatency()` returns the
last *finished* agent's connect duration. With `-hold=3m` inside a 3m30s
profile, no agent finishes during the walk, so every phase in every perf-stack
bundle carries `latency_p50_ms`/`p95`/`p99` as `null`. The field is `omitempty`
and nothing complains.

**D11 — profile `gates:` are never evaluated.**
[`profile.go`](../../server/tests/loadtest/profile.go) validates their shape and
nothing reads them. Every ceiling in all seven profiles is decorative, including
the ones marked `blocking: true`. `Phase.Sessions`
([`profile.go:134`](../../server/tests/loadtest/profile.go#L134)) is validated
and drives nothing, so `scaling.yaml`'s `sessions: 5` runs zero sessions.

### 1.5 What D6–D11 produce, observed

All four legs of the 2026-09-04 scaling sweep returned identical phase results:
offered equals achieved, latency null, error rate 0, faults 0. The only figures
that varied were connect p95 — 55 ms at 0.5 processors, 16 ms at 1, 22 ms at 2,
13 ms at 4. Non-monotonic, and no job compares them:

**D12 — the sweep has no consumer.** `perf-stack.yml` has two jobs and neither
reads the other's output. There is no publish job, no trend, no regression gate,
and the uploads use `if-no-files-found: warn`. Four bundles are produced nightly
and nothing has ever compared them.

**D13 — the volume family has one volume point.** The job runs
[`volume.yaml`](../../load/profiles/volume.yaml) once, at `fixture: lopsided`.
Varying how much data is already there is the family's whole definition. What it
currently produces is a fixture weight (1.61 MB for 500 devices), which is a
useful fact and is not a volume test.

### 1.6 Two methodology faults

**D14 — the workload model is closed-loop.** k6 `stages` and `constant-vus` hold
*users*, not *arrival rate*: each user waits for its own reply before sending
again, so when the server slows the offered load falls with it and latency
understates the damage. `operator_arrivals_per_second` in the profiles is the
open-loop model stated correctly and implemented nowhere (D6). k6's
`constant-arrival-rate` / `ramping-arrival-rate` executors are the direct fix.

**D15 — the top of the sweep is contended.** The bundle records
`generator.cpus: 4`, which is the whole runner. At the `cpus: "4"` leg the
server container alone claims all of it, alongside Postgres (1.0), VictoriaMetrics
(0.5) and the load generator running unlimited on the same host. The 2 and 4
points measure oversubscription, not processors.

### 1.7 Reliability

Sixteen of the last twenty Load Tests runs were red, though the last week is
mostly green. Seven of eleven Performance Stack runs were red. The 2026-09-04
scheduled load test went invalid — the fleet did not hold, the relay scenario
found no machine, and nothing was published. Those are holes in a 14-day window
that forms a baseline from three samples
([`loadtest-regression-check.sh`](../../scripts/loadtest-regression-check.sh),
`MIN_WINDOW_SAMPLES=3`).

Its tolerances — `LATENCY_REL_TOL=4.0`, `P99_REL_TOL=3.0`, `RPS_REL_TOL=0.65` —
are an accurate statement about a noisy measurement, and the noise is what §2
addresses. They are not to be tightened before the measurement is worth trusting.

## 2. Decisions needed

Nothing below is locked. These are the choices that change what gets built.

| # | Question | Recommendation |
|---|---|---|
| 1 | Does the load generator get exempted from the per-IP rate limit, so the nightly can push past 60 rps? | Yes — a source-address allowance for the in-cluster generator pod. Without it D1 stands whatever else changes. |
| 2 | Which unscheduled families come back, and on what cadence? | Soak weekly (8h, overnight); spike and breakpoint weekly; normal and peak nightly replacing the current profile-less run. |
| 3 | Does staging carry a breakpoint run, given it shares a node with production? | Yes, at `breakpoint.yaml`'s existing guardrails — they are already the lowest in the directory. Decision 8 of the prior plan (production requests equal limits) is the prerequisite and is already in place. |
| 4 | Does the scaling sweep move to a larger runner, or cap at 2 processors? | Cap at 2 on the 4-vCPU runner. A larger runner is a paid tier. |
| 5 | Does the volume family become a real sweep (small / large / lopsided) or stay a weighing? | Sweep. Three legs of the same matrix shape the scaling family already uses. |
| 6 | Do the k6 scenarios move to arrival-rate executors? | Yes, for api-baseline and concurrent-agents. relay-throughput stays VU-based — one session per iteration is the unit. |

## 3. Scope

### In scope

- D1–D15.
- Every dead measurement made real, each with a test that fails when it goes
  dead again.
- The unscheduled families given a schedule, or deleted.
- An aggregation job for the scaling and volume sweeps.
- Steady-state percentiles taken over the steady window rather than the whole
  run.
- Docs, ADR, ledger, register.

### Out of scope

- Tightening the regression tolerances. They are calibrated against the current
  noise; re-calibrating is a follow-on once WS1–WS4 have produced a fortnight of
  runs.
- Production as a load target.
- A tenant-creation API (the existing debt entry and its trigger stand).
- Moving off GitHub-hosted runners.

## 4. Workstreams

Each is a micro-plan's worth of work and is written to be handed out separately.
Order matters: WS1 comes first because every later verdict rests on it.

### WS1 — Make the dead measurements real (D6, D7, D8, D9, D10)

The harness measures what it claims, or says it could not.

1. **Attainment (D6).** The sequencer records the arrival rate it actually
   achieved. This means something has to *generate* operator arrivals — today
   nothing does — so `Fleet` grows an operator-arrival driver beside
   `HoldConnected`, and `AchievedArrivalsPerSecond` is counted from what it
   completed.
2. **Headroom (D7).** Read the generator's own processor and memory use across
   the run, the way `LocalNodeReading` already does for the node, and report the
   measured figure. A reading that could not be taken reports *not measured* and
   invalidates, per the rule that a guard answering yes when it cannot ask
   protects nothing.
3. **Target fingerprint (D8).** The processor and memory limits of the system
   under test are passed in and recorded. `perf-stack.yml` already knows them —
   they are the matrix value.
4. **Connected count (D9).** `forgetLocked` on every completion, not only on
   error.
5. **Phase latency (D10).** Sample a live round trip during the phase rather
   than the last finished agent's connect duration.

**Guard:** a test that asserts `AchievedArrivalsPerSecond != Offered` for a
fleet deliberately held below its offered rate; a test that a bundle whose
target fingerprint is the hardcoded `1/1` fails `Bundle.Validate()`; a test that
`Connected()` falls when an agent completes.

### WS2 — Evaluate the gates, or delete them (D11)

`Profile.Gates` is read against the bundle's rows and produces `GateBreaches`,
which `Classify` already consumes. `Phase.Sessions` drives concurrent sessions
or comes out of the schema.

**Guard:** a test that a profile with a deliberately breached blocking gate
classifies `failed`, and one whose gates are all clear classifies `valid`.

### WS3 — Schedule the missing families (D2, D3, D4, D5)

Per decision 2. Each family gets a workflow entry, a slot outside the 03:00–05:30
window where the 20-job pool is saturated, and a bundle upload with
`if-no-files-found: error`. `Testing.md`'s family table is rewritten to describe
what runs.

**Guard:** a shell test that every profile in `load/profiles/` is named by some
workflow, and that every family row in `Testing.md` names a workflow that exists.
This closes D5's blind spot — a present-tense claim about something unscheduled —
which the live-state gate structurally cannot see.

### WS4 — Give the sweeps a consumer (D12, D13, D15)

1. An aggregation job with `needs:` on the scaling matrix that downloads all
   legs, asserts the curve moves with the variable, and fails when it does not.
   Four identical bundles is the condition it exists to catch.
2. The volume job becomes a matrix over the three fixture sizes, aggregated the
   same way.
3. Cap the scaling sweep per decision 4, and record the runner's own shape
   beside the target's so contention is visible rather than inferred.
4. Uploads move to `if-no-files-found: error` now that something downstream
   reads them.

### WS5 — Load worth the name (D1, D14)

1. Per decision 1, the generator's source address is allowed past the per-IP
   limit, and `loadtest-rate-budget.test.sh` is re-pointed at whatever the new
   binding constraint is.
2. Per decision 6, api-baseline and concurrent-agents move to arrival-rate
   executors, so a slow server shows as a queue rather than as a quieter test.
3. Steady phases lengthen to 3–5 minutes and percentiles are taken over the
   steady window, not over the ramps as well.
4. Thresholds are re-based on what the widened load actually produces. Today's
   marks sit 15–100× above the measurement and distinguish nothing.

## 5. Verification

- `/precommit` green.
- The scheduled runs are the verdict, as in the prior plan. A green night after
  WS1 must show at least one figure that could not have been produced before:
  a measured headroom below 100, a target fingerprint that is not 1/1, an
  attainment that is not exactly 1.0.
- The scaling aggregation must fail on a deliberately identical set of legs.
- Break the product and watch a run go red, per
  [`test-value.md`](../rules/test-value.md): pin the server at 0.25 processors
  and require the sweep to report the curve flattening; hold a goroutine per
  session and require the conservation slope to red.

## 6. Reviewer checklist

- [ ] No number in a bundle is a literal. Every field either comes from a
      reading or is absent.
- [ ] Every validity rule in `validity.go` has a test that makes it fire.
- [ ] Every profile in `load/profiles/` is named by a workflow, or is deleted.
- [ ] `Testing.md`'s family table matches the workflow files.
- [ ] Every artifact something downstream reads is `if-no-files-found: error`.
- [ ] The regression tolerances are unchanged in this work.
- [ ] Docs, ADR, `decisions.md` row, `phases.md` row, `techdebt.md` entries for
      anything deferred.
- [ ] This plan archived in the commit that lands its final workstream.
