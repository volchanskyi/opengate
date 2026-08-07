# EF-A2 — Request-derived metric grids + retention drift reconciliation

**Master plan:** `edge-first-telemetry-and-investigations.md` §6.1 (A3, A5), §7.6 (I4), steps 2 and 4.
**Acceptance criteria owned:** **A3**, **A5**.
**Dependencies:** none (independent of EF-A1 — different files, different failure).
**Blocks:** nothing. Pairs with EF-A1 as "the chart tells the truth about the window it was asked for".

## Context — verified

**The grid defect.** [`assembleMetricRange`](../../../server/internal/api/metrics_assemble.go#L14)
builds its time axis from `unionGrid(avg)`
([:62](../../../server/internal/api/metrics_assemble.go#L62)) — the sorted union of timestamps
**VictoriaMetrics happened to return**. So a 7 d request over a device with 20 min of data renders
two points, indistinguishable from a 1 h request, and the window selector reads as a dead control
(measurement M1: 1 h→111 pts, 6 h→51, 24 h→14, 7 d→2, all from the same instant).

The step is already chosen from the request, not the data:
[`chooseStep`](../../../server/internal/api/handlers_device_metrics.go#L139) picks the smallest whole
second bucket ≥ `minRangeStepSecs` that keeps the count within `maxPoints`
(`defaultMaxPoints = 1000`, `maxMaxPointsBound = 2000`,
[handlers_device_metrics.go:22-25](../../../server/internal/api/handlers_device_metrics.go#L22-L25)),
and the response already carries it as `BucketS`. **No new response field is needed** — only the
axis must come from the same place the step does.

**The retention drift.** [values.yaml:26](../../../deploy/helm/monitoring/values.yaml#L26) declares
`retention: 90d`; the live statefulset runs `-retentionPeriod=30d`. The chart is the only input to
the rendered argument
([victoriametrics.yaml:98](../../../deploy/helm/monitoring/templates/victoriametrics.yaml#L98)), so
the chart is simply wrong, and nothing catches it.

## File inventory

- **Modify:** [metrics_assemble.go](../../../server/internal/api/metrics_assemble.go) — replace
  `unionGrid` with a request-derived grid builder; project VM's answer onto it.
- **Modify:** [handlers_device_metrics.go](../../../server/internal/api/handlers_device_metrics.go) —
  pass `from`/`to`/`step` into assembly.
- **Modify:** [handlers_device_metrics_test.go](../../../server/internal/api/handlers_device_metrics_test.go).
- **Create:** a `testvm`-backed alignment test (real VictoriaMetrics via
  [testvm](../../../server/internal/testvm/testvm.go)) — see step 1.
- **Modify:** [values.yaml](../../../deploy/helm/monitoring/values.yaml) — `retention: 30d`.
- **Create:** `scripts/tests/monitoring-retention.test.sh` — offline drift test, modelled on
  [monitoring-scrape.test.sh](../../../scripts/tests/monitoring-scrape.test.sh). **`chmod +x` (100755)
  or the gauntlet's shell-tests step fails.**
- **Modify:** [DeviceMetrics.tsx](../../../web/src/features/devices/DeviceMetrics.tsx) /
  [TimeSeriesChart.tsx](../../../web/src/features/devices/charts/TimeSeriesChart.tsx) — grid-length
  tolerance and no interpolation across nulls.
- **Docs:** [Monitoring.md](../../../docs/Monitoring.md), [API-Reference.md](../../../docs/API-Reference.md).

## Steps (TDD-first)

1. **Test first, against a real VM (`testvm`), because the alignment rule must be *measured*, not
   assumed:** write samples at known timestamps, issue a `query_range`, and assert the returned
   timestamps equal the grid the server computes for the same `(from, to, step)`. VictoriaMetrics'
   `query_range` start-alignment behaviour is what decides whether `t[0] = from` or
   `from` rounded to a step multiple — pin it with this test, then make the builder match. A grid
   that disagrees with VM by one bucket silently shifts every value by one bucket.
2. **Test first:** `assembleMetricRange` over a fixture where VM answers 2 of 1 008 buckets returns
   `len(t) == span/step` with `null` in every unanswered slot, `t[i] == t[0] + i*step`, and
   `i ∈ [0, span/step)` (the end instant is **exclusive**) → then implement.
3. **Test first:** a VM point whose timestamp is **not** on the grid is a defect, not a silent drop —
   assert it is counted (reuse the drop-counter idiom or a dedicated
   `opengate_metrics_grid_misalignment_total`) and logged, never discarded quietly. This is the
   WS-A lesson applied to the read path.
4. **Test first:** grid length never exceeds `maxMaxPointsBound` for any window (7 d, 30 d, 1 y) —
   `chooseStep` already bounds it; the test pins that the new builder cannot widen it.
5. **Web, test first:** a series of 1 008 buckets with 2 non-null values renders **gaps**, not a line
   across the hole (E4 — never interpolate), and the x-axis spans the full requested window. Assert
   uPlot's gap behaviour explicitly rather than trusting the default.
6. **Test first:** `scripts/tests/monitoring-retention.test.sh` fails on today's tree (90d), passes
   after the change. Assert **both** halves of the invariant offline (no `helm` on `$PATH`, no
   conditional skip): `values.yaml` declares `30d`, **and** the statefulset template passes exactly
   that value through `-retentionPeriod={{ .Values.victoriametrics.retention }}` with no second
   literal anywhere in the chart.
7. Docs: state the live retention and the request-derived grid contract.

## Traps

- `Downsampled: stepSecs > minRangeStepSecs` keeps its meaning only while `minRangeStepSecs` matches
  the real ingest cadence. **EF-B2 changes that constant to 60** — do not touch it here, and do not
  hard-code `10` anywhere new, or the two plans will collide.
- The band path (`attachBand`, [:47](../../../server/internal/api/metrics_assemble.go#L47)) aligns
  min/max onto the same grid — it must be re-pointed at the new grid, not left on `unionGrid`.
- A 30 d window at a 2 000-point cap means ~21 min buckets; that is correct and intended. Do not
  "fix" it by raising `maxMaxPointsBound` — response size is bounded on purpose.
- The retention test must **fail loudly** if the chart is unreadable; a grep that matches nothing
  must be a failure, not a pass (the `|| echo SKIP` anti-pattern).

## Out of scope

Cadence (EF-B2), what the dims mean (EF-B1), VM pod sizing (EF-B3).

## Reviewer checklist

- [ ] Grid comes from `(from, to, step)` only; `unionGrid`'s data-derived axis is gone from the range path.
- [ ] Alignment is pinned by a **real-VictoriaMetrics** test, not a mock.
- [ ] Off-grid VM points are counted and logged, never dropped silently.
- [ ] Charts render nulls as gaps; no interpolation across a device-offline window.
- [ ] `values.yaml` = `30d`; shell test is executable (100755) and asserts both halves offline.
- [ ] `/precommit` green; `make lint-k8s` still renders the monitoring chart clean.

## Verification

`cd server && go test ./internal/api/...` (incl. the `testvm` alignment test),
`cd web && npm test`, `bash scripts/tests/monitoring-retention.test.sh`, `make lint-k8s`,
then `/precommit`. Manual: a 7 d request on a device with 20 min of data renders seven days with one
short run of data and an honest hole.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row.
