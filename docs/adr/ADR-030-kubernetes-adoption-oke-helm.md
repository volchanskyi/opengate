# ADR-030: Kubernetes adoption — OKE, Helm, ingress-nginx, and cert-manager

**Status:** Accepted
**Date:** 2026-06-02
**Supersedes:** none

## Context

OpenGate runs on Oracle Kubernetes Engine within the OCI free-tier envelope.
The deployment needs a managed control plane, reproducible packaging, automated
TLS, persistent PostgreSQL, and direct L4 exposure for QUIC and Intel AMT MPS.

The current topology is one worker node and one server replica. Scale-out
requirements are retained separately in
[`Multiscale-Readiness.md`](../Multiscale-Readiness.md).

## Decision

1. **Cluster: OKE BASIC.** The managed BASIC control plane preserves worker
   capacity for the application. Worker configuration lives in the
   [`oke` Terraform module](../../deploy/terraform/modules/oke/).

2. **Packaging: Helm.** The application chart and its environment overlays live
   under [`deploy/helm/opengate`](../../deploy/helm/opengate/). CD deploys the
   chart with `helm upgrade --install` in
   [`cd.yml`](../../.github/workflows/cd.yml).

3. **HTTP edge: ingress-nginx + cert-manager.** The
   [`Ingress`](../../deploy/helm/opengate/templates/ingress.yaml) terminates TLS,
   routes the SPA and API, and applies the security headers owned by the chart.
   The server serves the SPA from its image.

4. **State: in-cluster PostgreSQL.** PostgreSQL runs as a
   [`StatefulSet`](../../deploy/helm/opengate/templates/postgres-statefulset.yaml).
   Production persistence and backup behavior are owned by the chart and
   [ADR-035](ADR-035-oke-free-tier-block-volume-remediation.md).

5. **L4 exposure: node `hostPort`.** QUIC and MPS bind directly to the current
   node through the server
   [`Deployment`](../../deploy/helm/opengate/templates/server-deployment.yaml).
   This avoids another load balancer and preserves the direct UDP/TCP path.

6. **Secrets: external.** The chart references an existing Kubernetes Secret;
   committed manifests contain no secret values. The expected shape is
   documented by
   [`secrets.example.yaml`](../../deploy/helm/opengate/secrets.example.yaml).

7. **Validation: rendered-manifest gates.** `make lint-k8s` runs Helm lint,
   schema validation, project policy, and Checkov through the canonical
   deployment lint path in the [`Makefile`](../../Makefile).

## Consequences

- Kubernetes, ingress, certificates, storage, and node upgrades are application
  operational concerns.
- The single-node L4 path cannot scale across workers; the rebuild requirements
  are specified in [`Multiscale-Readiness.md`](../Multiscale-Readiness.md).
- Shared server keys are supplied through the existing Secret as recorded in
  [ADR-034](ADR-034-scale-out-keda-shared-keys.md).
- Multi-node L4 templates and distributed relay dependencies are not carried in
  the current chart.
