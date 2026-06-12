# Kubernetes

OpenGate runs on **Oracle Kubernetes Engine (OKE)** via a Helm chart. This is
the Phase 13b deployment substrate that replaces the single-VM
[Docker Compose + Caddy stack](./Infrastructure.md) on the cluster path. The
platform decisions are recorded in
[ADR-030](./adr/ADR-030-kubernetes-adoption-oke-helm.md).

## Chart

The application chart is [`deploy/helm/opengate`](../deploy/helm/opengate/). Its
templates translate the compose services one-for-one:

| Compose service | Kubernetes object |
|---|---|
| `server` | Deployment + ClusterIP Service (HTTP) + hostPort L4 (QUIC/MPS) |
| `postgres` | StatefulSet + headless Service + `oci-bv` PVC |
| `postgres-backup` | CronJob (`pg_dump` → OCI Object Storage via a write-only PAR; [ADR-035](./adr/ADR-035-oke-free-tier-block-volume-remediation.md)) |
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
rationale.

### Shared keys

- **`sharedKeys.enabled`** — multi-replica correctness prerequisite. Switches
  `/data` from the per-replica RWO PVC to an `emptyDir` and mounts the enrollment
  CA, VAPID, and agent-update signing keypairs read-only from `existingSecret`, so
  every replica serves identical key material (the server loads keys if present —
  no code change). Generate the four key files once and fold them into the secret;
  recipe in [`secrets.example.yaml`](../deploy/helm/opengate/secrets.example.yaml).

The production overlay enables shared keys. The chart contains only the current
single-replica path.
[`Multiscale-Readiness.md`](./Multiscale-Readiness.md) retains the requirements
for rebuilding those capabilities when demand justifies them.

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
