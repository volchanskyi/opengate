# ADR-030: Kubernetes Adoption — OKE, Helm, ingress-nginx + cert-manager

**Status:** Accepted
**Date:** 2026-06-02
**Supersedes:** none (extends the deployment posture; ADR-023 remains the registry/multiserver spine)

## Context

Phase 13b moves OpenGate from a single-VM `docker compose` + Caddy stack to a
horizontally-scalable platform. PR-A wired the `SessionRegistry` port onto the
live relay path; the remaining PRs need a real Kubernetes cluster as the
substrate for ≥2 server replicas, a Redis session registry (PR-C), and an HPA
(PR-E). This ADR records the **deployment-platform** decisions PR-B commits to.
The §6 platform decisions (OKE flavour, in-cluster Postgres, in-place convert,
Redis Sentinel) were resolved 2026-06-01; the working sequencing lives in
`.claude/decisions.md` and the Phase 13b plan.

Hard constraints carried in: the OCI Always-Free envelope is **4 OCPU / 24 GB**
total; deployment starts on a single node and grows toward the full budget; the
agent transport (QUIC, UDP) and Intel AMT CIRA (MPS, TCP) are **non-HTTP L4**
protocols that cannot ride an HTTP ingress.

## Decision

1. **Cluster: OKE, BASIC tier.** The OKE control plane is free on BASIC;
   workers are the Always-Free A1.Flex. (kubeadm rejected — a self-managed
   control plane eats the 4-OCPU budget; non-OCI managed k8s leaves the free
   tier. ENHANCED OKE rejected — it bills per cluster-hour.)

2. **Packaging: Helm.** One chart, `deploy/helm/opengate`, with
   `values-staging.yaml` / `values-production.yaml` overlays mirroring the
   `docker-compose.yml` / `docker-compose.staging.yml` split. CD deploys via
   `helm upgrade --install`.

3. **Edge: ingress-nginx + cert-manager.** An `Ingress` terminates TLS and
   proxies `/`, `/api`, `/ws` to the server; cert-manager (`ClusterIssuer`,
   Let's Encrypt HTTP-01) automates ACME. The security headers (CSP, HSTS,
   X-Frame-Options, …) previously set by Caddy are ported **verbatim** to
   `more_set_headers` snippets on the ingress. Caddy is retired from the k8s
   path. The server already serves the SPA itself (`-web-dir`, `os.OpenRoot`
   fallback), so the former `web-init` initContainer + shared `web-assets`
   volume are dropped — the edge serves nothing static.

4. **State: in-cluster Postgres StatefulSet + PVC** on the `oci-bv` block-volume
   CSI driver; daily `pg_dump` via a `CronJob`. The server's `/data` (self-signed
   CA + VAPID keys) is a per-replica RWO PVC for the single-replica PR-B.

5. **L4 exposure: hostPort on the single node.** QUIC (9090/udp) and MPS
   (4433/tcp) bind directly to the node's public IP via `hostPort` — budget-free
   (no per-Service OCI Load Balancer) and source-IP-preserving, which AMT CIRA
   and QUIC require. The multi-node path (ingress-nginx `tcp-services`/
   `udp-services` ConfigMaps, or an OCI NLB) is templated but off by default.

6. **Secrets: external.** The chart references an `existingSecret`
   (JWT_SECRET, POSTGRES_PASSWORD, AMT_PASS, VAPID_CONTACT) created out-of-band;
   no secret material is committed. Sealed-secrets / SOPS layer on later
   (CD Phase E).

7. **Validation gate.** `policy/k8s/security.rego` (conftest, the project-native
   policy layer, mirroring `policy/docker_compose`) enforces image-tag hygiene,
   resource limits, run-as-non-root, and health probes against the rendered
   chart; `kubeconform -strict` schema-validates it; Checkov's `helm` framework
   adds residual coverage with documented `skip-check` entries (the helm
   framework renders to ephemeral temp dirs, so path-based `.checkov.baseline`
   is unusable — `skip-check` with justification is the analogue, per ADR-015).
   All three run in `make lint-k8s`, inside `make lint-deploy`, inside the
   precommit gauntlet and the CI `config-lint` job.

## Consequences

- **New operational surface:** a Kubernetes cluster (manifests, ingress, ACME,
  CSI volumes, node-pool upgrades). The in-place-convert migration is an
  OCI-provisioned node pool replacing the compose stack on the freed budget +
  DNS re-point — *not* a literal running-VM conversion (OKE provisions its own
  worker nodes).
- **Multi-replica blocker recorded:** the server's CA + VAPID keys live on a
  per-replica PVC. Scaling past one replica (PR-C/PR-E) requires promoting that
  material to a shared Secret, else replicas would mint divergent CAs and break
  enrolled agents. Tracked in techdebt.
- **Redis HA** (Sentinel) and the **cross-server proxy** land in PR-C with their
  own ADR superseding ADR-023's Redis-HA deferral. This ADR does not introduce
  Redis.
- Caddy remains the edge for the legacy single-VM compose stack until the
  cutover completes; the two coexist only during migration.
