# Kubernetes

OpenGate runs on **Oracle Kubernetes Engine (OKE)** via a Helm chart. This is
the Phase 13b deployment substrate that replaces the single-VM
[Docker Compose + Caddy stack](./Infrastructure.md) on the cluster path. The
platform decisions are recorded in
[ADR-030](./adr/ADR-030-kubernetes-adoption-oke-helm.md); the one-time cutover
steps live in the [Kubernetes Migration runbook](./Kubernetes-Migration.md).

## Chart

The application chart is [`deploy/helm/opengate`](../deploy/helm/opengate/). Its
templates translate the compose services one-for-one:

| Compose service | Kubernetes object |
|---|---|
| `server` | Deployment + ClusterIP Service (HTTP) + hostPort L4 (QUIC/MPS) |
| `postgres` | StatefulSet + headless Service + `oci-bv` PVC |
| `postgres-backup` | CronJob (`pg_dump` → backups PVC) |
| `caddy` | `Ingress` (ingress-nginx) + cert-manager `ClusterIssuer` |
| `web-init` + `web-assets` volume | *removed* — the server serves the SPA itself (`-web-dir`) |

Environment overlays mirror the compose split:
[`values-staging.yaml`](../deploy/helm/opengate/values-staging.yaml) and
[`values-production.yaml`](../deploy/helm/opengate/values-production.yaml). The
tunable surface is documented inline in
[`values.yaml`](../deploy/helm/opengate/values.yaml).

### Cluster prerequisites

Installed once per cluster, outside the chart (the chart's
[`NOTES.txt`](../deploy/helm/opengate/templates/NOTES.txt) prints the exact
commands): **ingress-nginx** (with `controller.allowSnippetAnnotations=true` so
the ported security-header snippets apply) and **cert-manager** (CRDs +
controller). The OKE cluster + node pool are provisioned by the
[`oke` Terraform module](../deploy/terraform/modules/oke/).

### Secrets

The chart never embeds secret material — it references an `existingSecret`
(`server.existingSecret`) created out-of-band. See
[`secrets.example.yaml`](../deploy/helm/opengate/secrets.example.yaml) for the
`kubectl create secret` recipe.

### L4 (QUIC + MPS)

QUIC (agent transport, UDP) and Intel AMT CIRA (MPS, TCP) are non-HTTP and
cannot ride the ingress. On the single-node start they bind to the node's
public IP via `hostPort` (`server.hostPortL4`) — see ADR-030 §5 for the
rationale and the multi-node alternative.

### Redis (distributed SessionRegistry)

The chart ships a **Redis Sentinel HA** topology (data StatefulSet + Sentinel
StatefulSet + headless Services) backing the multiserver `SessionRegistry`. It
is **dormant by default**: gated behind `redis.enabled` (off), with the server
defaulting to the in-process registry (`REGISTRY_BACKEND=inprocess`). When
enabled, the server is wired to the Sentinel service via `REGISTRY_BACKEND=redis`
+ `REDIS_SENTINEL_ADDRS` + `REDIS_MASTER_NAME`. The design, key schema, and the
"do not flip any overlay to `redis` until the C2 cross-server proxy lands"
constraint are recorded in
[ADR-031](./adr/ADR-031-redis-sentinel-session-registry.md); the tunables live in
[`values.yaml`](../deploy/helm/opengate/values.yaml) under `redis`. The
cross-server WebSocket proxy that the `redis` backend enables
([ADR-033](./adr/ADR-033-cross-server-relay-proxy-pod-ip.md)) and its Redis-loss
degraded-mode posture are exercised end-to-end by the multiserver harness —
see [Testing § Multiserver E2E](./Testing.md#multiserver-e2e-phase-13b-pr-d).

## Validation

`make lint-k8s` is the chart gate (wired into `make lint-deploy`, the precommit
gauntlet, and the CI `config-lint` job):

- `helm lint` + `helm template … | kubeconform -strict` (schema validation,
  CRDs ignored)
- `conftest verify`/`test` against [`policy/k8s`](../policy/k8s/) — image-tag
  hygiene, resource limits, run-as-non-root, health probes
- Checkov's `helm` framework (`make iac-policy`), residual findings tracked as
  documented `skip-check` entries in [`.checkov.yaml`](../.checkov.yaml)

See [Testing](./Testing.md) for the broader test-layer map and
[CI Pipeline](./CI-Pipeline.md) for where these run.
