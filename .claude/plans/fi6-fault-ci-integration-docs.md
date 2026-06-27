# Micro-Plan FI6 — CI integration, docs & rollout close-out

**Master:** `context-driven-fault-injection.md` §11 (FI6), §8 (CI/CD control model), §9 (observability), §13.
**Branch:** `dev`. **Owner:** engineer (CI + docs). **Sequence:** last. **Depends on:** FI2–FI5 (the injector, Helm config, ingress profiles, and k8s runner all exist).
**Status:** Ready after FI5.

## Goal

Wire the bounded `smoke` profile into staging CD after E2E (gating production
promotion), keep `infra`/`network` profiles scheduled/manual, upload evidence per
run, document the system, and close out the rollout (project-state + archive the
master plan).

## Context (verified)

- Staging deploys through the OKE job in
  [`cd.yml`](../../.github/workflows/cd.yml) using `kubectl port-forward` for
  staging smoke tests — Model C reaches the injector over **that** forward, never
  the public Ingress (master Decision 5, §8).
- Observability prerequisite: the live node scrape currently fails in
  VictoriaMetrics — **restore node metrics before** any infra scenario relies on
  CPU/mem/disk assertions (master §9; readiness doc). Verify
  `up`/node-exporter/`/metrics`/ingress-log queries live first.

## File inventory

**Create**
- `.github/workflows/fault-tolerance.yml` — `workflow_dispatch` with inputs
  restricted to an **enumerated** profile; a reusable entry callable **after**
  staging E2E; the existing OCI/kubeconfig action; a concurrency guard (no two
  overlapping staging fault runs); a hard timeout > the longest bounded scenario;
  artifact upload (assertions, logs, metrics, events).
- `docs/Fault-Injection.md` (extend) — deployment docs, profile
  ownership/cleanup/emergency-removal runbook.

**Modify**
- `.github/workflows/cd.yml` — invoke the `smoke` profile after E2E, gated by the
  repo variable `STAGING_FAULT_TESTS`; select subset via `STAGING_FAULT_PROFILE`.
- `.claude/phases.md` — add the completed fault-injection phase row.
- `.claude/techdebt.md` — clear/adjust any related entries; record the
  `network` (D1) deferral if still open.
- `.claude/plans/context-driven-fault-injection.md` → move to
  `.claude/plans/archive/` (bump its internal links one `../` deeper; validate
  with `go run ./scripts/check-doc-links`).
- `docs/Home.md` — ensure Fault-Injection is linked.

**Tests (write first — TDD)**
- `scripts/tests/fault-ci.test.sh` (+ `actionlint`) — assert: workflow inputs are
  enumerated (no free-form profile); the `smoke` invocation is gated by
  `STAGING_FAULT_TESTS`; concurrency guard present; a cleanup step runs under
  `always()`; the `network` profile is never wired into the gating path.

## Steps (TDD)

1. Branch current after FI5.
2. **Verify the observability prerequisite live** (node scrape / `up` / ingress
   logs) before relying on infra evidence.
3. **Test first:** the CI-policy test (enumerated inputs, gating var, concurrency,
   `always()` cleanup) — red.
4. Add `fault-tolerance.yml`; wire the `smoke` profile into `cd.yml` after E2E
   behind the repo variable; artifact upload.
5. Write the deployment docs + emergency-removal runbook.
6. Run the staging `smoke` profile green; confirm artifacts upload.
7. Update phases.md/techdebt.md; archive the master plan (fix links).
8. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## Acceptance criteria (master §13)

1. The `smoke` profile runs in staging CD after E2E and gates production promotion.
2. `infra`/`network` profiles run scheduled/manual and do **not** gate promotion.
3. Every workflow failure path removes fault resources and verifies no residue.
4. Each run uploads assertions, logs, metrics, and k8s events tagged by scenario ID.
5. No Redis/multiserver/cross-server scenario exists (consistent with teardown).
6. Full precommit gauntlet + the staging smoke profile pass.

## Reviewer checklist

- [ ] Workflow inputs are enumerated; no arbitrary profile/duration from dispatch.
- [ ] `smoke` reaches the injector via `kubectl port-forward` (Model C), not the
      public Ingress.
- [ ] Concurrency guard + hard timeout + `always()` cleanup present.
- [ ] Observability prerequisite verified live before infra evidence is trusted.
- [ ] Master plan archived with links fixed (`check-doc-links` green); phases.md
      + techdebt.md updated.
- [ ] `actionlint` clean; no secret echoed in workflow logs.
