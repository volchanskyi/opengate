# Micro-Plan B2: New Benchmark Trend Pipeline (Go + Rust)

**Parent master:** `benchmarks-grafana-trends.md` (§9 B2). **Branch:** `dev`.
**Owner:** CI/Bash + Go/Rust. **Depends on:** `bench-trends-b1-vm-transport.md`.
**Blocks:** nothing (parallel with B3/B4).

## 1. Goal

A nightly workflow that runs Go + Rust micro-benchmarks, detects regressions against a
**committed** baseline, pushes a PromQL-graphable trend to VM (via B1's `vm-push.sh`),
and Telegram-alerts on a deterministic regression. Modeled on
[`mutation.yml`](../../../.github/workflows/mutation.yml).

## 2. Scope

**In:** `benchmark.yml`, `benchmark-summarize.sh`, `benchmark-vm-push.sh`,
`benchmarks/baseline.json` (committed), `benchmark-trend.json` (PromQL dashboard).
**Out:** retiring the old gh-pages `bench-publish`/`go-bench`/`rust-bench` jobs (B5);
per-PR gating (explicitly dropped — noisy on shared runners).

## 3. File inventory

| File | Change |
|---|---|
| `.github/workflows/benchmark.yml` | **New.** Cron **04:00 UTC** + `workflow_dispatch` (with an `update-baseline` input). Jobs: **run** (Go+Rust matrix) → **publish** (`observability` env: summarize → regression check → Telegram on fail → `vm-push`). Mirror `mutation.yml`'s run/publish split and `if: always()` publish. |
| `scripts/benchmark-summarize.sh` | **New, sourceable + unit-tested.** Parse `go test -bench -benchmem` and `cargo bench -p mesh-protocol` (criterion JSON) into canonical rows: `{name, lang, ns_op, allocs_op, bytes_op}`. |
| `scripts/benchmark-vm-push.sh` | **New.** Map canonical rows → Prometheus text per the B1 convention (`*_ns_op`/`*_allocs_op`/`*_bytes_op`, labels `commit`,`env=ci`,`benchmark`,`lang`) → call `lib/vm-push.sh`. |
| `scripts/tests/benchmark-summarize.test.sh` | **New.** Behavioral test on fixture `go test`/criterion output (mirror [`pmat-summarize.test.sh`](../../../scripts/tests/pmat-summarize.test.sh)). |
| `benchmarks/baseline.json` | **New, committed.** Per-benchmark `allocs_op`/`bytes_op` baselines + tolerances. Refreshed only via the `update-baseline` dispatch mode + reviewed PR. |
| `deploy/grafana/provisioning/dashboards/benchmark-trend.json` | **New.** PromQL panels (datasource uid `VictoriaMetrics`) for ns/op (advisory), allocs/op + B/op (gated). |

## 4. Regression model (from master §6 — normative)

- **Primary gate (deterministic):** `allocs/op` + `B/op` vs `benchmarks/baseline.json`,
  tight tolerance (>1–2% over baseline ⇒ fail + Telegram). Runner-load independent.
- **Advisory only (noisy):** `ns/op` — graphed; alert only on a wide, sustained
  threshold (>1.5–2×); never reds the run on a single wobble.
- Baseline is the committed file, **not** a rolling VM query (avoids re-importing noise).

## 5. Approach (TDD)

1. B1 merged first (transport available).
2. **Red:** `benchmark-summarize.test.sh` on captured Go+criterion fixtures → canonical
   rows. Implement `benchmark-summarize.sh` to green.
3. Implement `benchmark-vm-push.sh` (uses `lib/vm-push.sh`); implement the
   baseline-diff regression check (a sourceable function with its own unit test: forced
   `allocs/op` bump ⇒ non-zero exit; `ns/op`-only wobble ⇒ zero exit).
4. Author `benchmark.yml` (run+publish) and `benchmark-trend.json`.
5. Commit a real `baseline.json` generated from a clean run via the `update-baseline`
   path.
6. `make shell-quality` + `/precommit` green per commit.

## 6. Acceptance criteria / DoD

- [ ] `benchmark.yml` runs nightly + on dispatch; a manual dispatch produces a VM series
      visible as a **PromQL trend in Grafana** (verify against the live cluster).
- [ ] A **forced `allocs/op` bump** reds the run and fires a **Telegram** alert; a
      synthetic `ns/op` wobble does **not** red the run.
- [ ] `update-baseline` dispatch regenerates `benchmarks/baseline.json` for PR review;
      normal runs never mutate it.
- [ ] `benchmark-summarize.sh` is sourceable + unit-tested; `make shell-quality` green.
- [ ] Full `/precommit` gauntlet green.

## 7. NFRs

- **Performance/cost:** nightly only (no per-PR tax); tiny VM series volume.
- **Reliability:** `if: always()` publish; canonical rows kept as a run artifact.
- **Maintainability:** reuses B1 transport + the mutation.yml shape.

## 8. Reviewer/QA checklist

- [ ] Gate is on `allocs/op`/`B/op` (deterministic), not `ns/op`.
- [ ] Baseline is committed and only mutated via the dispatch path.
- [ ] Dashboard uses datasource uid `VictoriaMetrics` and the B1 metric names.
- [ ] Regression function has a unit test proving both the fire and no-fire paths.
