# EF-B3 — Q2/Q3/Q4 measurement: what 120 000 active series actually costs

**Master plan:** `edge-first-telemetry-and-investigations.md` §2.3, §9.1 (Q2–Q4), §9.1's sizing
decision, D32, D36, step 6 (measurement half).
**Acceptance criteria owned:** **B2**.
**Dependencies:** EF-B2 (the vitals set must be final, or the measurement describes a shape that
does not ship).
**Blocks:** nothing. **Explicitly does not block** the rest of WS-B or WS-C.

## Objective

Replace a rule of thumb with a measurement, then **stop**. §1.5 caught "~2 KB/series RAM" being
cited as if measured; §9.1 forbids sizing the pod against that same figure. This plan produces a
number and hands §9.1's four options to the project owner. **It changes no limit and no manifest.**

## What is being measured

| # | Quantity | Method |
|---|---|---|
| Q2 | Active series at 5 000 agents × 24 vitals | `/api/v1/status/tsdb` against a real VictoriaMetrics holding the synthetic fleet |
| Q3 | **Marginal RAM per active series** | Linear fit of `process_resident_memory_bytes` against active series over ≥ 4 load points, so VM's fixed baseline is separated from the per-series slope |
| Q4 | Disk at 5 000 agents over 30 d | Measured `vm_rows_added_to_storage_total` and on-disk bytes at the vitals cadence, projected to 30 d (§2.2's measured 0.316 B/sample is the cross-check, not the source) |

A single-point RSS reading divided by series count is **not** an answer — it folds VM's baseline
into the slope and overstates per-series cost at small N. The fit is the deliverable.

## File inventory

- **Create:** `server/tests/vmramseries/` — a harness that provisions a dedicated throwaway
  VictoriaMetrics via [testvm](../../../server/internal/testvm/testvm.go), writes N devices × the real
  vitals set, settles, and scrapes VM's own `/metrics` plus `/api/v1/status/tsdb`.
- **Reference:** [spike_test.go](../../../server/tests/vmcardinality/spike_test.go) — the existing
  cardinality spike is the shape to follow (`run_id`-tagged writes, real VM, no mocks).
- **Deliverable:** a measurement record in the close-out evidence (EF-Z1) — raw table, fit, method,
  VM version, and the date.

## Steps (TDD-first)

1. **Test first:** the committed harness runs at a **small, always-on** scale (four load points that
   finish inside the normal suite budget) and asserts the *method*: series count matches what was
   written, RSS is read from a VM that is not shared with other tests, and the fit produces a
   positive slope with a reported R². No skips, no env gate — it always runs
   ([tests-determinism.md](../../rules/tests-determinism.md)).
2. Run the **full-scale** experiment manually with the same harness at 120 000 series: write
   5 000 × 24, let VM settle through a flush cycle, read RSS, active series, and disk.
3. Record the raw table and the fit. Cross-check the disk figure against §2.2's measured
   0.316 B/sample; a divergence larger than ~2× means the write shape is wrong, not the constant.
4. **Stop with the number.** Present §9.1's expectation table and the four options **to the project
   owner** — raise the limit / shrink the vitals set / lower the fleet target / accept as-is. Do not
   pick one.

## Traps

- **Do not change `deploy/helm/monitoring/values.yaml` VM resources in this plan.** The live limit is
  512 Mi and stays there until the owner decides (D32). A "helpful" bump here re-commits exactly the
  error §9.1 exists to prevent.
- Measure against a VM instance that is **not** shared with the rest of the Go suite; the suite now
  provisions one VictoriaMetrics per run, and another test's series would land in the fit.
- Report the VM version alongside the number. Per-series memory is a property of the build, so an
  undated, unversioned figure rots into the next rule of thumb.
- Exceeding the 400 MB Q3 budget **does not fail this step** (B2 says so explicitly) — it triggers
  the owner's decision.

## Out of scope

Any sizing change. Any change to the vitals set (that would be re-opening D3, which belongs to
EF-B2's cap test).

## Reviewer checklist

- [ ] Fit over ≥ 4 load points, not a single-point division; R² reported.
- [ ] Dedicated VM instance; `run_id`-scoped writes; no mocks.
- [ ] Committed harness always runs at the small scale with no skip conditions.
- [ ] Full-scale numbers recorded with method, VM version, and date.
- [ ] **No** manifest, values, or resource change in the diff.
- [ ] The four §9.1 options are put to the owner; none is applied unilaterally.

## Verification

`cd server && go test ./tests/vmramseries/...`, then the manual full-scale run. `/precommit` green.

## Close-out (mandatory)

`git mv` this plan to `archive/` in the landing commit, bump internal links one `../` deeper,
repoint the master-plan index row. The measurement record itself belongs in the EF-Z1 evidence
section, not only in this plan (an archived plan is deletion-bound).
