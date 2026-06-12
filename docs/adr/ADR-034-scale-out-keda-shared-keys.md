# ADR-034: Shared server keys via Secret

**Status:** Accepted
**Date:** 2026-06-05
**Extends:** [ADR-030](ADR-030-kubernetes-adoption-oke-helm.md)

## Context

The server loads persistent enrollment CA, web-push VAPID, and agent-update
signing material from `/data`. Generating that material independently on each
pod would break enrolled-agent trust, push delivery, and update verification.

The production deployment also avoids a dedicated server-data block volume, so
the key material must survive pod replacement outside the container filesystem.

## Decision

Use the chart's existing Kubernetes Secret as the source of the four server key
files.

When `server.sharedKeys.enabled` is set in
[`values-production.yaml`](../../deploy/helm/opengate/values-production.yaml),
the server [`Deployment`](../../deploy/helm/opengate/templates/server-deployment.yaml):

- uses a writable `emptyDir` for `/data`;
- mounts the CA certificate, CA key, VAPID file, and update-signing file
  read-only through `subPath`;
- skips the server-data PVC; and
- permits `RollingUpdate` when the L4 exposure mode also allows overlapping
  pods.

The server already loads existing key files, so the mechanism requires no
application-code branch. An RWX filesystem is rejected because the material is
write-once/read-many and already fits the Secret boundary.

## Removed Scale-Out Components

The KEDA `ScaledObject` and PodDisruptionBudget portions of the original
decision are reverted. Their templates, values, policy rules, and tests are not
part of the current chart.

Session-aware autoscaling remains the preferred design shape because relay load
is connection-bound rather than purely CPU-bound. It must be rebuilt only after
distributed session routing, multi-node L4 exposure, and multi-replica
availability are proven together. The complete dependency order is kept in
[`Multiscale-Readiness.md`](../Multiscale-Readiness.md).

## Consequences

- Server identity and signing material survive redeploys without a server-data
  block volume.
- Every future replica can consume identical key material.
- Key generation and Secret population remain an explicit deployment
  responsibility documented by
  [`secrets.example.yaml`](../../deploy/helm/opengate/secrets.example.yaml).
- Shared keys alone do not make the application horizontally scalable.
