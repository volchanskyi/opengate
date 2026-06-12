# TD4 — Remove scale-out Helm chart + policy

**Parent:** `dormant-scale-out-teardown.md` (§2 Helm chart).
**Execution order:** **4th** (after TD1).
**Status:** Completed.
**Risk:** Low-Medium — chart rendering + Rego policy; no live prod object uses these
(redis/keda/pdb/l4 all `enabled: false`, verified).

## Verified file inventory

| Path | Action | Verified anchor |
|---|---|---|
| `deploy/helm/opengate/templates/redis-statefulset.yaml`, `redis-sentinel-statefulset.yaml`, `redis-service.yaml`, `redis-sentinel-service.yaml`, `redis-config.yaml` | **Delete.** | all 5 exist |
| `deploy/helm/opengate/templates/server-scaledobject.yaml`, `server-pdb.yaml`, `l4-tcp-udp-configmap.yaml` | **Delete.** | all 3 exist |
| [`deploy/helm/opengate/templates/server-deployment.yaml`](../../../deploy/helm/opengate/templates/server-deployment.yaml) | Remove the Redis/proxy env block and the internal `containerPort`. | completed |
| [`deploy/helm/opengate/values.yaml`](../../../deploy/helm/opengate/values.yaml) | Remove the `redis:`, `autoscaling:`, `podDisruptionBudget:`, `l4:` blocks + internal/proxy knobs. **Keep** `sharedKeys:` (live in prod). | completed |
| [`deploy/helm/opengate/ci/test-values.yaml`](../../../deploy/helm/opengate/ci/test-values.yaml) | Remove scale-out enablement; retain shared-keys rendering. | completed |
| `deploy/helm/opengate/secrets.example.yaml` | **Verify-only / likely no-op:** grep found **no `REDIS_*` entries** at plan time. Remove only if any appear. | empirical correction |
| [`policy/k8s/security.rego`](../../../policy/k8s/security.rego) | Remove KEDA ScaledObject and PodDisruptionBudget rules. | completed |
| [`policy/k8s/security_test.rego`](../../../policy/k8s/security_test.rego) | Remove the corresponding test cases. | completed |

## Coordination

- **Requires TD3** (the deployment env it removes references `REGISTRY_BACKEND`/
  proxy secret that TD3 stopped *setting* in `main.go`; here we remove the chart
  side). Independent of TD1/TD2 code, but sequence after TD1 per master §5.
- The production overlay ([`values-production.yaml`](../../../deploy/helm/opengate/values-production.yaml))
  must still render: it sets `sharedKeys.enabled: true` (`:12`) — **keep**.

## Steps (gauntlet green per commit)

1. **Test-first:** edit `security_test.rego` to drop Rule 5/6 cases (the
   test-change satisfying the gate).
2. Delete the 8 templates; edit `server-deployment.yaml`, `values.yaml`,
   `ci/test-values.yaml`; remove Rego Rules 5/6.
3. `make lint-k8s` — render + validate **every overlay** (default, staging,
   production, ci) with the templates/policy gone; conftest/opa green.
4. Grep guard: `grep -rnE 'redis|ScaledObject|PodDisruptionBudget|l4|REGISTRY_BACKEND|9091' deploy/helm policy/` returns only intended/doc hits.
5. `/precommit` → commit → `/refactor` → push.

## Reviewer checklist

- [x] All 8 scale-out templates deleted; deployment and service expose no retired internal/L4 path.
- [x] Redis helper definitions removed from `_helpers.tpl`.
- [x] `values.yaml` keeps `sharedKeys`, drops redis/autoscaling/pdb/l4; overlays render.
- [x] Rego Rules 5 & 6 + their tests removed; remaining rules pass.
- [x] `make lint-k8s` green on all overlays; production still renders with `sharedKeys`.
- [x] Full `/precommit` gauntlet green.

## Done when

`helm template` for every overlay renders cleanly with no redis/keda/pdb/l4
objects and the policy suite passes without Rules 5/6.
