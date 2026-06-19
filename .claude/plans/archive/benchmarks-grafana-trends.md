# Master Plan: Unify CI Trend Pipelines on VictoriaMetrics

**Covers:** new Go+Rust **benchmark** trends, migration of the existing **mutation /
pmat / terraform-drift** pipelines off Loki, and new **load-test** trend persistence —
all pushing numeric series to **VictoriaMetrics** with PromQL Grafana dashboards and
Telegram regression alerts.

**Type:** Master plan. **Broken into micro-plans B1–B6** (see §13) — implement from
those, not this file.
**Status:** Decision-complete; micro-plans drafted (B1–B6); awaiting per-plan approval.
**Branch:** `dev`.

---

## 1. One-paragraph summary

Make **VictoriaMetrics the single canonical store for all CI trend data**, returning
Loki to its proper role (logs only). Three strands: (1) **new** standalone nightly
`benchmark.yml` (Go+Rust micro-benchmarks); (2) **migrate** the existing nightly trend
pipelines — mutation, pmat, terraform-drift — from their Loki push to VM; (3) **add**
load-test trend persistence (today it stores nothing). All push numeric series through
one shared `scripts/lib/vm-push.sh` transport (the same authenticated `kubectl`-curl-pod
mechanism the Loki pipelines use today — no new exposure, no Pushgateway), visualised
as **PromQL** Grafana dashboards, with **Telegram** regression alerts in the
`observability` environment. Retention is **30 days** (confirmed sufficient). Then
**retire** the gh-pages benchmark path and the Loki-push trend path entirely. Start
fresh (no backfill).

---

## 2. Decisions locked by the requester

| # | Decision | Source |
|---|---|---|
| Store | **VictoriaMetrics** is the canonical CI-trend store. The **family (mutation/pmat/terraform-drift) and load tests** also write metrics to VM and build trends there. | this round |
| Retention | **30-day** trend window is sufficient (VM-only; no committed-canonical archival). | this round |
| Benchmark scope | **Go + Rust micro-benchmarks** (the new pipeline). | clarifying Q2 |
| gh-pages | **Retire entirely** — `github-action-benchmark` job + gh-pages data + the per-push bench jobs that only fed it. Never a blocking gate. | clarifying Q3 |
| Shape | Trend pipelines are **separate nightly workflows** (cron + `workflow_dispatch`), modelled on [`mutation.yml`](../../../.github/workflows/mutation.yml). | clarifying Q3 |
| Alerting | **Telegram** regression alerts (not PR comments). | clarifying Q3 |
| History | **Start fresh.** No backfill; clean up gh-pages + Loki-push history/scripts. | clarifying Q4 |
| Bench baseline | **Committed** `benchmarks/baseline.json`, refreshed via a `workflow_dispatch` "update-baseline" mode + reviewed PR. | this round |
| Load-test alert | **Trends-only** — the existing k6 threshold gate stays the only alarm; no new latency-regression alert. | this round |
| Family migration | **Single combined PR** for mutation + pmat + terraform-drift (verify all three VM trends before retiring the Loki pushes). | this round |

**Out of scope (fast-follow):** web perf (Lighthouse + bundle-size, the other gh-pages
consumer via `Publish Performance Data`).

---

## 3. Store decision — the research behind "VM, not Loki"

### 3.1 What feeds each backend today (verified across all workflows/scripts)
- **VictoriaMetrics** ← `vmagent` **scrapes** runtime metrics only (k8s nodes/pods +
  `opengate-server /metrics`), [`vmagent-scrape.yaml`](../../../deploy/helm/monitoring/files/vmagent-scrape.yaml).
  Nothing from CI pushes to VM today. Retention **30d** global.
- **Loki** ← CI nightly originally pushed mutation, PMAT, and terraform-drift
  rows through the now-retired `scripts/lib/loki-push.sh` transport. Historical
  retention was configured in
  [`loki-config.yml`](../../../deploy/helm/monitoring/files/loki-config.yml).
- **Load test** ([`load-test.yml`](../../../.github/workflows/load-test.yml)) ← stores
  **nothing**; k6 + a QUIC harness with pass/fail thresholds. No trend persisted.
- **github-action-benchmark** ← gh-pages static page (being retired).

### 3.2 Why the family chose Loki (history — archived PR9 plan)
[`pr9-mutation-testing-as-observability.md`](pr9-mutation-testing-as-observability.md)
originated the pattern **pre-Kubernetes** (docker-compose VPS), for expedience not
correctness: Loki was already reachable and push-friendly ("no new ports, no new
ingress, no Caddy auth"), and they explicitly wanted to **"not add a Pushgateway."** A
companion 90-day committed JSONL canonical was later **dropped**, leaving only a 14-day
window. pmat/terraform-drift then *copied* mutation. Path-dependence, not a "Loki is
right" decision.

### 3.3 Why VM is correct (and the Loki reasons no longer hold)
- These are **numeric series**; PromQL has the regression primitives (`deriv`,
  `predict_linear`, `quantile_over_time`, `*_over_time`) LogQL `unwrap` lacks. Benchmarks
  especially (`ns/op`, `allocs/op`, `B/op` × many benchmarks) are clearly metric-shaped.
- The **only** technical reason PR9 avoided VM — needing a **Pushgateway** — is **void**:
  VM has native push (`POST /api/v1/import/prometheus`, v1.114.0) and on Kubernetes is
  reachable via the **same** authenticated kubectl-curl-pod transport as Loki.
- Loki's **14d** retention is inadequate; VM gives a proper TSDB with a **30d** window
  (confirmed sufficient) and a clean separation: **Loki = logs, VM = metrics** (both
  scraped runtime *and* pushed CI trends).

### 3.4 Ingestion path (shared)
CI authenticates with [`oci-kube-setup`](../../../.github/actions/oci-kube-setup); a new
**`scripts/lib/vm-push.sh`** (sibling of `lib/loki-push.sh`) runs a throwaway curl pod
POSTing Prometheus-text to `http://<vm-svc>.monitoring.svc:8428/api/v1/import/prometheus`.
**No new inbound exposure** (VM stays ClusterIP), no Pushgateway, no new datasource
(Grafana already has the VictoriaMetrics datasource,
[`datasources.yml`](../../../deploy/grafana/provisioning/datasources/datasources.yml)).

### 3.5 Options considered
- **A — VM via kubectl transport ✅ recommended/locked.**
- **B — stay on Loki** — rejected: wrong store for numeric trends, 14d retention, weak
  LogQL aggregation.
- **C — Infinity over gh-pages** — moot (gh-pages retired).
- **D — public VM/Pushgateway ingress** — rejected; reverses ADR-035's
  no-monitoring-ingress posture for no gain over A.

---

## 4. Scope

**In:**
1. **New benchmark pipeline** — Go (`go test -bench -benchmem`) + Rust (`cargo bench -p
   mesh-protocol`, criterion); nightly workflow; VM push; PromQL dashboard; committed
   regression baseline; Telegram alert.
2. **Migrate the family to VM** — mutation, pmat, terraform-drift: swap their Loki push
   for `vm-push`, convert their trend dashboards LogQL→PromQL, keep their existing
   summarize/regression/Telegram logic.
3. **Load-test trend persistence** — extract k6 + QUIC-harness summary metrics
   (latency p50/p95/p99, throughput, error rate) → VM push → PromQL load-test trend
   dashboard (its threshold pass/fail gate stays).
4. **Retire** the gh-pages benchmark path and the Loki-push trend path (scripts +
   transport + Loki trend dashboards) once each migration is verified in VM.

**Out (fast-follow):** web perf (Lighthouse + bundle-size); per-PR benchmark gating
(dropped — noisy on shared runners, §6).

---

## 5. Expected output / deliverables

**Shared foundation**
- `scripts/lib/vm-push.sh` — shared VM import transport + the behavioral
  [`vm-transport.test.sh`](../../../scripts/tests/vm-transport.test.sh).
- A small Prometheus-text **metric convention**: `*_ns_op` / `*_allocs_op` /
  `*_bytes_op` (benchmarks), `mutation_score` / `pmat_*`, `terraform_drift_*`,
  `loadtest_latency_*` / `loadtest_rps` / `loadtest_error_rate`, all with a `commit`
  + `env="ci"` label and pipeline-specific labels.

**Benchmark pipeline (new)**
- [`.github/workflows/benchmark.yml`](../../../.github/workflows/) — nightly **04:00 UTC**
  + dispatch; run (Go+Rust matrix) → publish (`observability` env → summarize +
  regression → Telegram → VM push), mirroring `mutation.yml`.
- `scripts/benchmark-summarize.sh` + `scripts/benchmark-vm-push.sh`;
  `benchmarks/baseline.json` (committed; refreshed via a `workflow_dispatch`
  "update-baseline" mode).
- [`deploy/grafana/provisioning/dashboards/benchmark-trend.json`](../../../deploy/grafana/provisioning/dashboards/) (PromQL).

**Family migration**
- Replace `scripts/{mutation,pmat,terraform-drift}-loki-push.sh` with `*-vm-push.sh`
  (calling `lib/vm-push.sh`); rewrite [`mutation-trend.json`](../../../deploy/grafana/provisioning/dashboards/mutation-trend.json)
  + `pmat-trend.json` (+ a terraform-drift panel) to PromQL; update the three workflows'
  push step. Summarize/regression/Telegram unchanged.

**Load test**
- A `scripts/loadtest-summarize.sh` (parse k6 `--summary-export` JSON + QUIC harness
  output → canonical metrics) + `loadtest-vm-push.sh`; a `loadtest-trend.json` PromQL
  dashboard; push step added to `load-test.yml`.

**Retirement**
- Remove `bench-publish` + `go-bench`/`rust-bench` from [`ci.yml`](../../../.github/workflows/ci.yml);
  fix `needs:` in `notify-failure`/`merge-to-main`; purge gh-pages benchmark data; drop
  the `github-action-benchmark` action pin.
- After all migrations verified: delete `scripts/lib/loki-push.sh` +
  `*-loki-push.sh` + `loki-transport.test.sh`; remove the old LogQL trend dashboards.
  (Loki itself stays — logs.)

**Docs**
- ADR (VM as the canonical CI-trend store; the VM-vs-Loki rationale) + `decisions.md`
  row + `phases.md`; pay down the [`techdebt.md`](../../techdebt.md) "Performance
  benchmarks — no CI regression detection" entry; document the metric convention in
  `docs/Monitoring.md`.

---

## 6. Quality metrics & regression model

**Empirical problem:** wall-clock `ns/op` (benchmarks) and latency (load test) on shared
GitHub runners are **load-sensitive/noisy** — this repo has already relaxed exactly such
thresholds (load-test p(95) 50ms→250ms; "drop runner-load-sensitive wall-clock
thresholds"; gremlins timeout fragility, [`techdebt.md`](../../techdebt.md)).

**Model:**
- **Benchmarks — primary gate (deterministic):** `allocs/op` + `B/op` (runner-load
  independent), tight tolerance (>1–2%). **Advisory (noisy):** `ns/op`, graphed +
  alerted only on a wide tolerance (>1.5–2× sustained). **Baseline:** committed
  `benchmarks/baseline.json` (not a rolling query that re-imports noise).
- **mutation/pmat:** keep their existing regression rules (absolute floors / drop-pp) —
  the migration changes only the *store + dashboard*, not the detection.
- **load test:** the k6 pass/fail thresholds remain the **only** alarm; the VM trend is
  for visibility only — **no** latency-regression alert (shared-runner latency is too
  noisy to gate on).

**Quality of the deliverable:** new Bash passes `make shell-quality` + has behavioral
tests under [`scripts/tests/`](../../../scripts/tests/); push/summarize scripts are
source-able + unit-tested like `mutation-summarize`; `/precommit` green per commit.

---

## 7. Non-functional requirements

- **Security:** no new inbound exposure — VM stays ClusterIP; channel is the
  authenticated Kube API via `oci-kube-setup`; OCI + Telegram secrets scoped to the
  `observability` environment; no secrets in trend data.
- **Performance / cost:** nightly (no per-PR tax); CI trend volume is tiny vs VM's
  budget; one shared transport, no new component.
- **Maintainability:** **one** correct pattern for the whole family + benchmarks + load
  test (a single `vm-push.sh`, one PromQL dashboard style) instead of cloned Loki
  scripts and two backends for trends.
- **Reliability:** `if: always()` publish jobs + canonical-row artifacts (audit trail);
  the combined family-migration PR verifies all three VM trends **before** B5 removes
  any Loki push.

---

## 8. Architectural constraints & retention (settled)

- Free-tier OKE: VM 10Gi, **30d global retention** — **confirmed sufficient** for all CI
  trends; runtime scraped metrics already live at this retention, so adding low-volume CI
  series is negligible. No per-series retention needed; no committed-canonical archival.
- Grafana is **port-forward-only**; dashboards provision from the `grafana-dashboards`
  ConfigMap (ADR-035 emptyDir Grafana) — same access as every existing dashboard.

---

## 9. Workstreams (basis for micro-plan breakdown)

- **B1 — Shared VM transport.** `scripts/lib/vm-push.sh` + behavioral test + the metric
  naming/label convention. Foundation for all others. TDD.
- **B2 — Benchmark pipeline (new).** `benchmark-summarize.sh`, `benchmarks/baseline.json`,
  `benchmark-vm-push.sh`, `benchmark.yml`, `benchmark-trend.json`. TDD.
- **B3 — Migrate the family (single combined PR).** mutation, pmat, terraform-drift →
  `vm-push` + PromQL dashboards in one PR; regression/Telegram unchanged; verify all
  three VM trends before B5 drops the Loki pushes.
- **B4 — Load-test trends.** `loadtest-summarize.sh` + `loadtest-vm-push.sh` +
  `loadtest-trend.json` + push step in `load-test.yml`.
- **B5 — Retire legacy.** gh-pages benchmark path (`bench-publish`, feeders, action pin,
  history) + Loki-push scripts/transport/test + old LogQL trend dashboards; fix `needs:`.
- **B6 — Docs.** ADR + `decisions.md` + `phases.md` + `docs/Monitoring.md`; pay down the
  techdebt benchmark entry.

## 10. Sequencing & gating

B1 first (everything depends on the shared transport) → **B2 / B3 / B4 in parallel**
(independent pipelines on the shared transport) → **B5 only after** each migrated/new
pipeline has produced a verified VM trend (don't delete a Loki push or the gh-pages path
until its VM replacement is green) → B6 documents the landed state. Each micro-plan keeps
the gauntlet green per commit; TDD throughout.

## 11. Reviewer acceptance criteria (master)

- [ ] `scripts/lib/vm-push.sh` is the single CI trend transport; behavioral-tested.
- [ ] Benchmarks, mutation, pmat, terraform-drift, and load test each show a **PromQL
      trend in Grafana** sourced from VM (verified against the live cluster).
- [ ] A deterministic benchmark regression (forced `allocs/op` bump) fires a **Telegram**
      alert and reds the run; a noisy `ns/op`/latency wobble does **not**.
- [ ] gh-pages benchmark path **and** the Loki-push trend path fully removed (scripts,
      transport, old dashboards, action pin, history) with `needs:` graphs fixed; Loki
      retained for logs; runtime VM scraping untouched.
- [ ] ADR + `decisions.md` + `phases.md` + `docs/Monitoring.md` updated; techdebt entry
      paid down; `/precommit` green.

## 12. Planning decisions — resolved

All open questions are settled (recorded in §2): benchmark baseline = **committed**
`benchmarks/baseline.json`; load test = **trends-only** (no new alert); family
migration = **single combined PR**. The plan is decision-complete and ready for the
B1–B6 micro-plan breakdown on approval.

## 13. Micro-plan breakdown (engineer-ready)

Each is self-contained (file inventory, TDD steps, acceptance/DoD, reviewer checklist).
Referenced by filename (active plans cannot be linked; all live in `.claude/plans/`).

| WS | Micro-plan file | Depends on | Gate |
|---|---|---|---|
| B1 | `bench-trends-b1-vm-transport.md` | — | foundation; blocks B2–B4 |
| B2 | `bench-trends-b2-benchmark-pipeline.md` | B1 | parallel with B3/B4 |
| B3 | `bench-trends-b3-migrate-family.md` | B1 | single combined PR |
| B4 | `bench-trends-b4-loadtest-trends.md` | B1 | parallel with B2/B3 |
| B5 | `bench-trends-b5-retire-legacy.md` | B2,B3,B4 **verified** | hard gate: no Loki/gh-pages deletion until VM trends green |
| B6 | `bench-trends-b6-docs.md` | B2–B5 | documents landed state |

**Sequencing:** B1 → (B2 ∥ B3 ∥ B4) → B5 (after each VM trend verified) → B6.
