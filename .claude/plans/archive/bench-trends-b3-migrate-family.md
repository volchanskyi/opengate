# Micro-Plan B3: Migrate the Family (mutation / pmat / terraform-drift) to VM

**Parent master:** `benchmarks-grafana-trends.md` (§9 B3). **Branch:** `dev`.
**Owner:** CI/Bash. **Depends on:** `bench-trends-b1-vm-transport.md`.
**Shape:** **single combined PR** (locked decision — verify all three VM trends before
B5 removes any Loki push).

## 1. Goal

Move the three existing nightly trend pipelines off the Loki push and onto VM, changing
**only the store + dashboard** — their summarize/regression/Telegram logic is unchanged.

## 2. Scope

**In:** swap each pipeline's `*-loki-push.sh` for a `*-vm-push.sh` (calling
`lib/vm-push.sh`); convert each trend dashboard LogQL→PromQL; update each workflow's push
step.
**Out:** deleting the Loki scripts/transport/dashboards (B5, after verification);
changing detection thresholds.

## 3. File inventory

| File | Change |
|---|---|
| `scripts/mutation-vm-push.sh` | **New** — replaces [`mutation-loki-push.sh`](../../../scripts/mutation-loki-push.sh); emits `mutation_score` (+ existing labels) as Prometheus text via `lib/vm-push.sh`. |
| `scripts/pmat-vm-push.sh` | **New** — replaces [`pmat-loki-push.sh`](../../../scripts/pmat-loki-push.sh); `pmat_*` series. |
| `scripts/terraform-drift-vm-push.sh` | **New** — replaces [`terraform-drift-loki-push.sh`](../../../scripts/terraform-drift-loki-push.sh); `terraform_drift_*` series. |
| [`.github/workflows/mutation.yml`](../../../.github/workflows/mutation.yml), [`pmat-trend.yml`](../../../.github/workflows/pmat-trend.yml), [`terraform-drift.yml`](../../../.github/workflows/terraform-drift.yml) | Replace the Loki-push step with the VM-push step. Leave [`mutation-summarize.sh`](../../../scripts/mutation-summarize.sh) / [`pmat-summarize.sh`](../../../scripts/pmat-summarize.sh) / [`terraform-drift-summarize.sh`](../../../scripts/terraform-drift-summarize.sh) and the regression/Telegram steps **unchanged**. |
| `deploy/grafana/provisioning/dashboards/*-trend.json` | Rewrite the mutation + pmat trend dashboards LogQL→PromQL; add a terraform-drift PromQL panel. Datasource uid `VictoriaMetrics`. |
| `scripts/tests/*-vm-push.test.sh` (as needed) | Behavioral tests for each new push wrapper. |

## 4. Approach (TDD)

1. B1 merged.
2. For each pipeline: write a behavioral test asserting the new wrapper emits the
   correct Prometheus series/labels for a fixture summary, then implement the wrapper.
3. Swap the workflow push step; **keep the Loki push temporarily** so both run during
   verification (or run the new push alongside) — the combined PR must show all three
   VM trends green **before** B5 deletes the Loki side.
4. Convert dashboards to PromQL; verify each renders from VM against the live cluster.
5. `make shell-quality` + `/precommit` green.

## 5. Acceptance criteria / DoD

- [ ] mutation, pmat, terraform-drift each push to VM and render a **PromQL trend** in
      Grafana (verified live).
- [ ] Detection unchanged: each pipeline's existing regression rule + Telegram still
      fires on its existing condition (no threshold drift).
- [ ] New push wrappers are behavioral-tested; `make shell-quality` green.
- [ ] The Loki push path is still present (removal is B5) but the PR documents that all
      three VM trends are verified.
- [ ] `/precommit` green.

## 6. NFRs

- **Maintainability:** three pipelines collapse onto one transport + one dashboard style.
- **Reliability:** dual-run/verify before B5 removes the old path (no trend gap).
- **Security:** unchanged channel; no secrets in series.

## 7. Reviewer/QA checklist

- [ ] Summarize/regression/Telegram steps are byte-for-byte unchanged (diff shows only
      the push step + dashboards).
- [ ] All three VM trends shown green before sign-off; screenshot/PromQL attached.
- [ ] Dashboards use uid `VictoriaMetrics` and the B1 metric names.
- [ ] No Loki deletion in this PR (that is B5).

## 8. Risks

- LogQL→PromQL panel parity (label cardinality differences). Verify each panel renders.
- Coordinate with `td-gremlins-timeout-stability.md` (touches mutation pipeline scoring,
  not its store) — land order either way, rebase.
