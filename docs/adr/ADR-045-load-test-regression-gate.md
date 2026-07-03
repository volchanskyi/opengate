---
adr: 045
title: Load-Test Regression Gate from VictoriaMetrics Read-Back
status: Accepted
date: 2026-07-02
supersedes: ADR-038 (load-test visibility-only consequence only)
---

# ADR-045: Load-Test Regression Gate from VictoriaMetrics Read-Back

## Status

Accepted.

This ADR supersedes only ADR-038's consequence that load-test trends remain
visibility-only. [ADR-038](ADR-038-victoriametrics-ci-trend-store.md) remains the
accepted decision for VictoriaMetrics as the canonical numeric CI trend store.

## Context

Load-test trend rows already flow into VictoriaMetrics through
[`load-test.yml`](../../.github/workflows/load-test.yml),
[`loadtest-summarize.sh`](../../scripts/loadtest-summarize.sh), and
[`loadtest-vm-push.sh`](../../scripts/loadtest-vm-push.sh). Until now that data
was dashboard-only: the existing k6 thresholds and the QUIC harness exit code
were the only pass/fail signals.

That left slow drift invisible to CI unless it crossed a scenario's built-in
threshold. The shared VM read-back helper
[`scripts/lib/vm-query.sh`](../../scripts/lib/vm-query.sh) now provides
per-series window reads, so the workflow can compare each
`{source, scenario, phase}` series independently instead of collapsing a
multi-dimensional load-test surface into one scalar.

## Decision

Add a load-test regression gate in the publish phase of the load-test workflow.

[`load-test.yml`](../../.github/workflows/load-test.yml) now has two jobs:

- `load-test` runs the existing k6 and QUIC workload without a GitHub
  environment, preserving unattended scheduled runs, and uploads the canonical
  summary artifact.
- `publish` uses the `observability` environment, reads the summary artifact,
  runs [`loadtest-regression-check.sh`](../../scripts/loadtest-regression-check.sh),
  pushes the current rows to VictoriaMetrics, sends Telegram on regression, and
  fails the workflow red after the push/alert path has had a chance to run.

The gate is implemented in
[`loadtest-regression-check.sh`](../../scripts/loadtest-regression-check.sh):

- `latency_p50_ms`, `latency_p95_ms`, and `rps` compare each current series to a
  VictoriaMetrics window median with calibrated frozen tolerance, plus
  source/scenario/phase absolute ceilings or floors.
- `error_rate` uses an absolute ceiling and, when a previous sample exists, a
  previous-sample relative check.
- `latency_p99_ms` is advisory-only. It is pushed, shown in summaries, and named
  beside true latency regressions, but it never sets the regression exit status.
- Cold-start series use only absolute rules until enough VM history exists.
- VictoriaMetrics or kubectl failures are fail-open for the window rules; infra
  read-back trouble does not create a false regression.

## Consequences

- Load-test trends are no longer visibility-only: the scheduled/dispatchable
  load-test workflow now has its own regression audit trail and Telegram alert.
- The gate remains independent of `merge-to-main`; it turns the load-test
  workflow red, not the normal push/PR CI graph.
- Existing k6 and QUIC pass/fail behavior remains intact. The regression gate
  adds slow-drift detection after the run, rather than replacing scenario
  thresholds.
- New or historically absent series can start without false reds because missing
  VM history falls back to absolute-only evaluation.
- The tolerance bands are intentionally broad because staging load tests cross
  GitHub-hosted runners, kubectl port-forwards, and the OKE cluster. They catch
  material regressions while avoiding normal runner/network variance.
