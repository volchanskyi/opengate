# Phase 13b PR-E — scale-out (HPA/KEDA + PDB + shared keys)

**Created:** 2026-06-05 · **Parent:** [phase-13b-multiserver-scaling.md](phase-13b-multiserver-scaling.md) §4 PR-E · **Status:** Completed (E0–E4 landed; ADR-034). Archived 2026-06-05.

> **Outcome:** all slices landed, `make lint-k8s` green across all three value files (16 Rego tests). Notable correction: the KEDA prometheus query is `sum(opengate_relay_active_sessions)` — the metric is `opengate_relay_active_sessions` (namespace `opengate` + name `relay_active_sessions`), not `opengate_active_sessions`. No Go change (server loads-keys-if-present). Completes **Phase 13b (PRs A–E)** and thus **Phase 13**.

## Context

PR-C landed the multi-server registry + cross-server proxy; PR-D proved them e2e. PR-E is the "pool" half: let the server Deployment run **>1 replica** and autoscale. The blocker is the explicit pay-down trigger in [techdebt.md](../techdebt.md): `/data` (an RWO PVC) holds **three per-replica keypairs** — enrollment **CA** (`ca.crt`/`ca.key`), **VAPID** (`vapid.json`), and agent-update **signing** (`update-signing.json`) — plus the manifest cache. A second replica would mint its own keys → mTLS enrollment, web push, and update-manifest verification all split. So a *correct* scale-out must share those keys first.

Decisions (2026-06-05): **scaffold + shared-keys paydown** (make scale-out actually correct, clear the Medium debt) and **CPU + custom `opengate_active_sessions` via KEDA**. Everything ships default-off / cutover-gated and lint-validated, consistent with PR-B/C.

**No Go change:** the server already *loads keys if present* (`cert.NewManager`, `LoadOrGenerateVAPID`, `LoadOrGenerateSigningKeys` all read `{dataDir}/<file>` and only generate when absent). Mounting the four files read-only into `/data` makes every replica load the same material — purely a chart change.

## Slices

### E0 — shared keys (paydown), gated `server.sharedKeys.enabled` (default false)
- When enabled: `/data` becomes an `emptyDir` (writable, for the manifest cache); the four key files are mounted **read-only via `subPath`** from `server.existingSecret` at `/data/ca.crt`, `/data/ca.key`, `/data/vapid.json`, `/data/update-signing.json`; deployment `strategy` flips `Recreate`→`RollingUpdate` (the PVC-contention reason for Recreate is gone); `server-data-pvc.yaml` is skipped.
- `secrets.example.yaml` documents the four new secret keys; the runbook documents generating them once (run the server once / one-shot `kubectl run`, extract `/data`).

### E1 — KEDA autoscaling, gated `server.autoscaling.enabled` (default false)
- New `server-scaledobject.yaml`: a KEDA `ScaledObject` targeting the server Deployment with **two triggers** — `cpu` (utilization) and `prometheus` (querying VictoriaMetrics for `opengate_active_sessions`). `minReplicaCount`/`maxReplicaCount` bounded by the 4 OCPU/24 GB budget. KEDA owns replicas, so the Deployment **omits `spec.replicas`** when autoscaling.enabled. KEDA is a cutover prerequisite (documented); the ScaledObject is a `keda.sh` CRD so kubeconform skips it (`-ignore-missing-schemas`) and conftest Rego validates its shape.

### E2 — PodDisruptionBudget, gated `server.podDisruptionBudget.enabled` (default false)
- New `server-pdb.yaml`: `policy/v1` PDB, `minAvailable` (default 1), selecting the server pods. Only meaningful with >1 replica.

### E3 — Grafana per-replica session distribution
- Add a timeseries panel to the canonical [`opengate-overview.json`](../../deploy/grafana/provisioning/dashboards/opengate-overview.json) plotting `opengate_active_sessions` **by instance/pod** (the monitoring chart mounts this file, so it propagates to the k8s overlay too).

### E4 — policy + lint + ADR + docs + techdebt paydown
- `policy/k8s/security.rego` + `security_test.rego`: rules for `ScaledObject` (min ≤ max, ≥1 trigger) and `PodDisruptionBudget` (declares minAvailable or maxUnavailable). New conftest unit tests.
- `ci/test-values.yaml` enables `sharedKeys` + `autoscaling` (keda) + `podDisruptionBudget` so the scale-out path renders under `make lint-k8s`; staging/production keep the defaults (single-replica PVC path) so **both** code paths are validated across the 3-file matrix.
- New **ADR-034** (scale-out: KEDA autoscaler + shared-keys-via-Secret) + [decisions.md](../decisions.md) row.
- [docs/Kubernetes.md](../../docs/Kubernetes.md) scale-out section + `docs/Kubernetes-Migration.md` (removed) cutover steps (install KEDA, populate the keys secret).
- [techdebt.md](../techdebt.md): mark the per-replica CA/VAPID/signing-keys debt **paid** (mechanism shipped + lint-validated; runtime-verified at cutover like the rest of the dormant k8s path).

## Out of scope
Live multi-node cluster verification (cutover); NetworkPolicy for the internal listener (separate techdebt item, same cutover gate).

## Verification
`make lint-k8s` green across all three value files (helm lint + kubeconform + conftest, incl. the new Rego tests). Full `/precommit` per commit.
</content>
</invoke>
