---
adr: 038
title: VictoriaMetrics as the canonical CI trend store
status: Accepted
date: 2026-06-19
supersedes: ADR-017 and ADR-019 (CI trend-store clauses only)
---

# ADR-038: VictoriaMetrics as the Canonical CI Trend Store

## Status

Accepted.

This ADR supersedes only the CI trend-storage clauses in
[ADR-017](ADR-017-ci-gates-consolidation.md) and
[ADR-019](ADR-019-pmat-quality-overlay.md). Their workflow, quality-gate, and
alerting decisions remain accepted.

## Context

Numeric CI telemetry had split across two legacy paths: micro-benchmark history
on `gh-pages` and JSON trend rows pushed into Loki. Runtime metrics already used
VictoriaMetrics, while Loki's durable role was application and cluster logs.

The split made numeric trends use LogQL or branch-hosted JSON even though
VictoriaMetrics already provided numeric series, PromQL range functions, and a
Grafana datasource. It also duplicated transport and dashboard patterns across
the same monitoring stack.

The benchmark-trends rollout introduced a shared VictoriaMetrics transport,
migrated benchmark, mutation, PMAT, terraform-drift, and load-test telemetry,
and verified those live series before removing the legacy writers. The
implementation sequence and verification gates are recorded in the
[archived master plan](../../.claude/plans/archive/benchmarks-grafana-trends.md).

## Decision

### VictoriaMetrics owns numeric CI trends

VictoriaMetrics is the canonical store for benchmark, mutation, PMAT,
terraform-drift, and load-test trend series. Their workflow-to-metric mappings
are executable in the family push wrappers under [`scripts/`](../../scripts/)
and pinned by the focused shell tests in
[`scripts/tests/`](../../scripts/tests/).

The shared transport is
[`scripts/lib/vm-push.sh`](../../scripts/lib/vm-push.sh). It validates the
project-wide labels before importing Prometheus text through the authenticated
Kubernetes API path. Family wrappers own any additional labels and units.

### Loki owns logs

Loki remains the log store fed by the chart's
[`promtail-config.yaml`](../../deploy/helm/monitoring/files/promtail-config.yaml).
CI workflows no longer push numeric trend rows into Loki, and PMAT reads its
previous-run baseline from VictoriaMetrics through
[`pmat-vm-query.sh`](../../scripts/pmat-vm-query.sh).

The old Loki push transport, family wrappers, and LogQL trend queries are
retired. Loki's runtime deployment, persistence, Promtail path, and log
dashboards are unchanged.

### Branch-hosted benchmark history is retired

The benchmark feeder and publisher jobs are removed from
[`ci.yml`](../../.github/workflows/ci.yml). The remaining `gh-pages` deployment
owner removes the obsolete benchmark directory while preserving API docs and
the separate Lighthouse/bundle-size history.

### Retention and audit trail

CI trends use VictoriaMetrics' global **30-day retention**, configured beside
its persistent storage in the monitoring chart
[`values.yaml`](../../deploy/helm/monitoring/values.yaml). The expected CI
series volume is negligible relative to runtime scrape data, so no per-series
retention or second archive is introduced.

Canonical-row workflow artifacts remain the one-off audit surface where a
pipeline emits them. They are not a second trend database.

## Consequences

- Numeric CI dashboards use one PromQL/VictoriaMetrics pattern.
- Regression and Telegram rules remain pipeline-owned; changing the store does
  not change their thresholds or pass/fail semantics.
- Load-test trends remain visibility-only; their existing k6 and QUIC checks
  remain the only pass/fail gates.
- Loki has a single responsibility as the log backend.
- CI trend history follows the same retention and availability envelope as the
  cluster's runtime metrics.

## References

- [B5 legacy-retirement plan](../../.claude/plans/archive/bench-trends-b5-retire-legacy.md)
- [B6 documentation plan](../../.claude/plans/archive/bench-trends-b6-docs.md)
- [Monitoring and observability](../Monitoring.md)
