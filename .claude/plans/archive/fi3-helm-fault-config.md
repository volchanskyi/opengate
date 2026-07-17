# Micro-Plan FI3 — Helm configuration & production-deny

> **Status: Obsolete — superseded by the no-ship pivot ([ADR-055](../../../docs/adr/ADR-055-fault-injection-mechanism.md)). Never implemented; retained for history.**
> This micro-plan targeted the abandoned compiled-in-injector model — a server-side
> `faultInjection` env block plus a `FAULT_INJECTION_TOKEN`. Under ADR-055 the shipped
> binary carries **zero** fault code, so there is no server fault env to wire or to deny,
> and FI2 ([`faulttest`](../../../server/internal/faulttest/)) consumes nothing from env.
> FI3's production-deny goal is met **by construction**:
> [`fault_noship_test.go`](../../../server/tests/integration/fault_noship_test.go) proves
> the production build graph excludes `faulttest`. Deployed-fault production-deny lives in
> FI4 (ingress annotations, staging-only) and FI5 (Chaos Mesh namespace scope +
> production-pod-exclusion guard). Everything below is the pre-pivot text.

**Master:** `context-driven-fault-injection.md` §11 (FI3), §5 (Helm enablement), §13.
**Branch:** `dev`. **Owner:** engineer (Helm + Go). **Sequence:** after FI2. **Depends on:** FI2 (the server reads the enable flag/profiles from env).
**Status:** Ready after FI2.

## Goal

Expose fault injection through Helm values, wire it to the server env, and make
**production rendering fail** if fault injection is enabled — enforced by a chart
policy/template test, not just convention.

## Context (verified)

- Chart: [`deploy/helm/opengate`](../../../deploy/helm/opengate) (ADR-030); staging
  values [`values-staging.yaml`](../../../deploy/helm/opengate/values-staging.yaml);
  rendered-manifest validation runs through `make lint-k8s`.
- Production and staging share **one** worker; the production server binds the
  QUIC/MPS host ports — fault config must target the **staging** namespace only
  (master §4).

## File inventory

**Create**
- `deploy/helm/opengate/templates/tests/fault-injection-guard.yaml` *(or a
  `fail`-based guard in `_helpers.tpl`)* — render-time assertion that
  `faultInjection.enabled` is false unless `faultInjection.environment == staging`,
  and **always** false for the production values file.

**Modify**
- `deploy/helm/opengate/values.yaml` — add the disabled-by-default block:
  ```yaml
  faultInjection:
    enabled: false
    environment: staging
    secretKey: FAULT_INJECTION_TOKEN
    profiles: {}
  ```
- `deploy/helm/opengate/values-staging.yaml` — staging may enable it; token via
  the existing external Secret (ADR-034), never inline.
- `deploy/helm/opengate/templates/deployment.yaml` — map the values to server
  env (enable flag, environment, profiles JSON, token from Secret) consumed by
  FI2.
- `deploy/helm/opengate/templates/NOTES.txt` — note the staging-only constraint.

**Tests (write first — TDD)**
- `scripts/tests/helm-fault-injection.test.sh` (or extend the existing k8s
  render test) — assert: (a) default render has injection disabled and **no**
  fault env; (b) production values + `faultInjection.enabled=true` → `helm
  template` **fails**; (c) staging render wires the env + token-from-Secret ref;
  (d) the token is a Secret ref, never a literal.

## Steps (TDD)

1. Branch current after FI2.
2. **Test first:** add the render-policy test (red — guard doesn't exist).
3. Add the `faultInjection` values + the production-deny guard template.
4. Wire env in `deployment.yaml`; confirm FI2 reads it.
5. `make lint-k8s` + the new render test green.
6. `/precommit` → commit → `/refactor` → `/precommit` → commit → push.

## Acceptance criteria (master §13)

1. Default + production renders have fault injection disabled with zero fault env.
2. Production rendering **fails** if fault injection is enabled.
3. Staging render wires the enable flag, environment, profiles, and a
   **Secret-referenced** token (no inline secret).
4. `make lint-k8s` green.

## Reviewer checklist

- [ ] Production-deny is enforced at **render time** (`helm template` fails), not
      just documented.
- [ ] Token sourced from the existing external Secret; never a literal in values.
- [ ] Default state is disabled; no fault env appears when disabled.
- [ ] Staging-only environment gate matches the FI2 fail-closed contract.
- [ ] No resource-limit regressions; injector adds no always-on container.
