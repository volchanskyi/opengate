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
