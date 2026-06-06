# ADR-034: Server scale-out — KEDA autoscaling and shared keys via Secret

**Status:** Accepted
**Date:** 2026-06-05
**Extends:** [ADR-030](ADR-030-kubernetes-adoption-oke-helm.md) (the OKE/Helm
substrate and the single-replica server Deployment) and
[ADR-031](ADR-031-redis-sentinel-session-registry.md) /
[ADR-033](ADR-033-cross-server-relay-proxy-pod-ip.md) (the distributed registry +
cross-server proxy that make >1 replica *functionally* correct). This ADR records
the Phase 13b PR-E scale-out decisions. Nothing in those ADRs is changed.

## Context

PR-B shipped the server as a **single replica** with `/data` on a per-replica
`ReadWriteOnce` PVC. That `/data` holds three keypairs the server generates on
first boot and then reads: the self-signed enrollment **CA** (`ca.crt`/`ca.key`),
the web-push **VAPID** keypair (`vapid.json`), and the agent-update **signing**
keypair (`update-signing.json`). Running a second replica with that layout splits
all three: agents enrolled against replica A fail mTLS against B, push
subscriptions fork, and an update manifest signed by A can't be verified when B
serves it. The PR-B techdebt named PR-E as the explicit pay-down trigger:
"`server.replicas` must stay 1 and HPA must not raise it" until the keys are
shared.

PR-C/PR-D then made cross-server sessions correct (Redis registry + proxy), so the
only remaining blocker to horizontal scale is the per-replica key material plus an
autoscaler. The monitoring stack already exposes `opengate_relay_active_sessions`
(a per-pod gauge) in VictoriaMetrics — a natural session-aware scale signal.

## Decision

**1. Shared keys via the existing `Secret`, not a shared filesystem.**
`server.sharedKeys.enabled` (default false) switches `/data` from the RWO PVC to a
writable `emptyDir` (manifest cache) and mounts the four key files **read-only via
`subPath`** from `server.existingSecret` at their expected `/data/<file>` paths.
The server already *loads keys if present* — no Go change. The rollout strategy
flips `Recreate`→`RollingUpdate` (no PVC contention to avoid). Keys are generated
once out-of-band (run the single-replica PVC path once, extract `/data`, fold into
the Secret) — the same `existingSecret` pattern PR-B uses for JWT/Postgres
credentials. A `ReadWriteMany` filesystem (OCI File Storage) was rejected: new
infra and cost for material that is write-once-read-many and already secret-shaped.

**2. KEDA `ScaledObject` as the autoscaler, two triggers.**
`server.autoscaling.enabled` (default false) renders a single KEDA `ScaledObject`
with a `cpu` (Utilization) trigger and a `prometheus` trigger querying
`sum(opengate_relay_active_sessions)` from VictoriaMetrics, targeting
~`activeSessionsPerReplica` sessions per pod. KEDA owns the underlying HPA, so the
Deployment **omits `spec.replicas`** when autoscaling is on. A plain CPU-only
`HorizontalPodAutoscaler` was rejected: relay load is session-bound, not
CPU-bound, so CPU alone would scale late; KEDA gives the custom-metric trigger
without a bespoke prometheus-adapter `APIService`. KEDA is a cutover prerequisite
(documented in the migration runbook); `keda.sh` is a CRD, so kubeconform skips it
(`-ignore-missing-schemas`) and `policy/k8s` validates its shape instead.

**3. PodDisruptionBudget.** `server.podDisruptionBudget.enabled` (default false)
renders a `policy/v1` PDB (`minAvailable: 1`) so node drains / rollouts keep at
least one server serving — only meaningful multi-replica.

**4. Dormant + lint-validated, like the rest of the k8s path.** All three flags
default off; staging/production overlays keep the single-replica PVC path, while
`ci/test-values.yaml` turns them on so `make lint-k8s` (helm + kubeconform +
conftest, incl. new ScaledObject/PDB Rego rules) validates the scale-out path.
Runtime behaviour is verified at cutover, consistent with ADR-030/031/033.

## Consequences

- The per-replica CA/VAPID/signing-key techdebt is **paid down** (mechanism
  shipped + lint-validated); enabling >1 replica now requires only populating the
  Secret and installing KEDA at cutover.
- Per-replica session distribution is observable via a new "Active Relay Sessions
  by Replica" panel on the Grafana overview dashboard (`opengate_relay_active_sessions`
  by instance), which doubles as the visual for the KEDA scale signal.
- New cutover prerequisites: the KEDA operator must be installed, and the
  `existingSecret` must carry the four key files. Both are documented in the
  migration runbook.
- The internal-listener NetworkPolicy gap (ADR-033 techdebt) is unchanged and
  remains gated on the same cutover.
