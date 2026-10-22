---
number: 30
title: Kubernetes on OKE, packaged with Helm
---

# ADR-030 — Kubernetes on OKE, packaged with Helm

## Decision

**Oracle Container Engine, BASIC tier.** The managed control plane keeps the
worker nodes inside the free allowance, which is the whole budget.

**Helm is the packaging.** One application chart with per-environment overlays,
in [`deploy/helm/`](../../deploy/helm/).

**ingress-nginx and cert-manager at the HTTP edge**, so certificates renew
without anyone watching them.

**The repository holds the ingress controller's settings.** Its values live in
[`deploy/ingress-nginx/values.yaml`](../../deploy/ingress-nginx/values.yaml) and
its chart version in the tool manifest.
[`ingress-controller-apply.sh`](../../deploy/scripts/ingress-controller-apply.sh)
installs them from the approval-gated `cluster` job in
[`cd.yml`](../../.github/workflows/cd.yml), whenever a commit changes them, since
staging and production share the one controller and the routine deploys' login
cannot write cluster-wide objects ([ADR-128](./ADR-128-machine-identity-lifecycle.md)); the nightly configuration job
in [`terraform-drift.yml`](../../.github/workflows/terraform-drift.yml) reads
them back and fails on any difference. The load balancer's subnet is Terraform's
and is read from the running Service, because another subnet would move the load
balancer to a new address. Snippet annotations are off: the controller's account
lists Secrets cluster-wide, so a snippet could read every one (CVE-2021-25742).
The server sets every security header on its own responses, so the controller
carries none. A one-time hand fix with a nightly check, a nightly apply, and
feeding the controller a headers ConfigMap were weighed and rejected: the first
leaves the install outside the repository, the second changes a shared
controller outside the production approval, and the third needs a second copy
of the headers held equal to the server's, for nginx's own error pages alone.
cert-manager is installed by hand once per cluster, as the chart's notes print.

**PostgreSQL runs in the cluster** as a StatefulSet in the same chart
([ADR-014](ADR-014-postgresql.md)).

**QUIC and the AMT transport bind to a node port directly.** They are not HTTP,
so the HTTP edge cannot carry them, and a second load balancer is not in the
budget.

**Secrets are external to the chart.** The chart references an existing
Kubernetes Secret; nothing secret is templated.

**Rendered manifests are gated.** `make lint-k8s` renders the chart and runs the
policy scanners over the output, because a chart that lints is not the same as
a chart that renders something valid.

## Consequences

Binding to a node port ties those two services to the node's address, which the
deploy reads from the cluster rather than assuming
([ADR-086](ADR-086-deploy-reads-the-cluster.md)).
