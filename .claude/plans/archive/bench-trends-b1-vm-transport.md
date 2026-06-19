# Micro-Plan B1: Shared VictoriaMetrics Push Transport

**Parent master:** `benchmarks-grafana-trends.md` (§9 B1). **Branch:** `dev`.
**Owner:** CI/Bash. **Depends on:** nothing. **Blocks:** B2, B3, B4.

## 1. Goal

One reusable transport that POSTs Prometheus-text metrics to VictoriaMetrics over the
existing authenticated kubectl-curl-pod channel — the VM sibling of the later
retired `scripts/lib/loki-push.sh`. Every other CI trend
pipeline (B2–B4) calls it; no per-pipeline transport is written again.

## 2. Scope

**In:** `scripts/lib/vm-push.sh` + its behavioral test + the shared metric
naming/label convention.
**Out:** any pipeline-specific metric production (B2–B4 own that); deleting
`loki-push.sh` (B5 owns retirement).

## 3. File inventory

| File | Change |
|---|---|
| `scripts/lib/vm-push.sh` | **New.** Sourceable. Function `vm_push <prom_text_file>` (or stdin) → throwaway `kubectl run` curl pod → `POST http://opengate-victoriametrics:8428/api/v1/import/prometheus`. Mirror `loki-push.sh`'s pod lifecycle, error handling, and cleanup; only the URL, HTTP path, and payload format (Prometheus text, not Loki JSON streams) differ. |
| `scripts/tests/vm-transport.test.sh` | **New.** Mirror the later retired `loki-transport.test.sh`: stub `kubectl`, assert the request targets the VM import endpoint, the payload is well-formed Prometheus text, labels are attached, and a non-2xx response fails loudly. |
| `docs/Monitoring.md` | Add the **metric convention** (see §4) — but only the convention; B6 owns the broader doc pass. |

## 4. Metric & label convention (normative — B2–B4 must follow)

- Names: `*_ns_op`, `*_allocs_op`, `*_bytes_op` (benchmarks); `mutation_score`,
  `pmat_*`, `terraform_drift_*`; `loadtest_latency_*`, `loadtest_rps`,
  `loadtest_error_rate`.
- Mandatory labels on every series: `commit="<sha>"`, `env="ci"`, plus
  pipeline-specific labels (e.g. `benchmark="<name>"`, `lang="go|rust"`).
- Timestamps: omit (VM stamps at ingest) unless backfilling.

## 5. Approach (TDD — Bash)

1. Read `loki-push.sh` + `loki-transport.test.sh` to copy the pod/auth/cleanup pattern.
2. **Write `vm-transport.test.sh` first** (red): stub `kubectl`, call `vm_push` with a
   fixture, assert endpoint + payload + label injection + non-2xx failure.
3. Implement `vm-push.sh` until green. Keep it `set -euo pipefail`, sourceable, no
   secret echoed.
4. `make shell-quality` (shellcheck + shfmt + behavioral tests) green.
5. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## 6. Acceptance criteria / Definition of Done

- [ ] `scripts/lib/vm-push.sh` exists, is sourceable, and POSTs Prometheus text to
      `…:8428/api/v1/import/prometheus` via a throwaway pod (no new inbound exposure).
- [ ] `scripts/tests/vm-transport.test.sh` covers: correct endpoint, well-formed
      payload, label injection, **and** non-2xx → non-zero exit (failure is loud).
- [ ] The metric convention (§4) is documented in `docs/Monitoring.md`.
- [ ] `make shell-quality` and full `/precommit` gauntlet green.
- [ ] No secret value (OCI/kube token) is logged by the script.

## 7. NFRs

- **Security:** reuses the authenticated Kube API channel; VM stays ClusterIP; no
  secrets in payload or logs.
- **Maintainability:** single transport; B2–B4 must not reimplement push.

## 8. Reviewer/QA checklist

- [ ] Diff shows `vm-push.sh` mirrors `loki-push.sh` structure (pod cleanup on failure,
      `trap`-based teardown).
- [ ] Test stubs `kubectl` (no live-cluster dependency); runs deterministically.
- [ ] Endpoint and payload format verified against VM `import/prometheus` (not Loki's).
- [ ] Convention §4 is the same one B2–B4 reference (grep the sibling plans).
