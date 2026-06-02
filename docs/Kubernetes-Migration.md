# Kubernetes Migration Runbook

One-time cutover of the OpenGate production stack from the single-VM
[Docker Compose + Caddy deployment](./Infrastructure.md) to
[OKE](./Kubernetes.md). Platform decisions: [ADR-030](./adr/ADR-030-kubernetes-adoption-oke-helm.md).

This is an **operator runbook** — it requires OCI credentials and live cluster
access, and it modifies production. Run it deliberately, top to bottom, with a
maintenance window.

## Safe-to-merge vs cutover-gated changes

PR-B's repo changes split by blast radius:

| Change | When to merge |
|---|---|
| App chart [`deploy/helm/opengate`](../deploy/helm/opengate/) | Anytime — additive, doesn't touch prod |
| OKE module [`deploy/terraform/modules/oke`](../deploy/terraform/modules/oke/) | Anytime — not wired into the root stack |
| Monitoring chart `deploy/helm/monitoring` | Anytime — additive |
| `cd.yml` deploy swap (compose → `helm`), staging-E2E steps | **At cutover only** — breaks the live compose deploy if merged early |
| Loki-push retarget (`scripts/*-loki-push.sh`) | **At cutover only** — the SSH→docker path serves the live compose monitoring until then |

The cutover-gated automation lands together with steps 4–6 below, once the
cluster is serving traffic.

## 0. Prerequisites

- OCI creds with OKE + networking + block-volume permissions.
- The OKE-compliant subnets/NSGs the [`oke` module](../deploy/terraform/modules/oke/)
  expects (API-endpoint + node + optional LB), wired into the networking module
  / root, plus the `oci-bv` CSI default StorageClass (OKE installs it).
- Resolve `kubernetes_version` and `node_image_id` (commands in the
  [oke module README](../deploy/terraform/modules/oke/README.md)).

## 1. Provision the cluster

```sh
# Wire the oke module into the root stack (or apply it as a standalone config),
# supplying the networking inputs, then:
terraform -chdir=deploy/terraform apply           # creates OKE BASIC cluster + 1-node A1.Flex pool
oci ce cluster create-kubeconfig --cluster-id "$(terraform output -raw oke_cluster_id)" \
  --file ~/.kube/config --region "$OCI_REGION" --token-version 2.0.0
kubectl get nodes                                 # 1 node Ready
```

Install the edge prerequisites (the chart's
[NOTES.txt](../deploy/helm/opengate/templates/NOTES.txt) prints exact commands):
**ingress-nginx** (`controller.allowSnippetAnnotations=true`) and **cert-manager**
(CRDs + controller).

## 2. Migrate the database (pg_dump → restore)

The DB data does a pilot-then-cutover even though compute is in-place (ADR-030):

```sh
# On the VM: dump the live compose Postgres.
docker exec opengate-postgres pg_dump -U opengate -Fc opengate > /tmp/opengate.dump

# Create the namespace + the required Secret (see secrets.example.yaml), then
# bring up just Postgres via the chart so the StatefulSet PVC is provisioned:
kubectl create namespace opengate
kubectl -n opengate create secret generic opengate-secrets --from-literal=... # JWT/POSTGRES/AMT/VAPID
helm upgrade --install opengate deploy/helm/opengate -n opengate \
  -f deploy/helm/opengate/values-production.yaml --set server.replicas=0

# Restore into the StatefulSet pod.
kubectl -n opengate cp /tmp/opengate.dump opengate-postgres-0:/tmp/opengate.dump
kubectl -n opengate exec -i opengate-postgres-0 -- \
  pg_restore -U opengate -d opengate --clean --if-exists /tmp/opengate.dump
```

## 3. Deploy the app (staging first, then production)

```sh
helm upgrade --install opengate-staging deploy/helm/opengate -n opengate-staging \
  --create-namespace -f deploy/helm/opengate/values-staging.yaml --set image.tag="$TAG"
# validate, then production:
helm upgrade --install opengate deploy/helm/opengate -n opengate \
  -f deploy/helm/opengate/values-production.yaml --set server.replicas=1 --set image.tag="$TAG"
kubectl -n opengate rollout status deploy/opengate-server
```

Open the QUIC (9090/udp) and MPS/AMT (4433/tcp) `hostPort`s in the node's NSG and
point agents / AMT at the node's public IP (ADR-030 §5).

## 4. Migrate the monitoring stack

The seven-service [observability stack](./Monitoring.md) moves into a
`monitoring` namespace via the `deploy/helm/monitoring` chart:

- **VictoriaMetrics** (StatefulSet) — scrape config switches from the compose
  static targets in `deploy/victoriametrics/scrape.yml` to `kubernetes_sd`
  service discovery.
- **Loki** (StatefulSet) + **promtail** (DaemonSet) — promtail's config changes
  from tailing `/var/lib/docker/containers` to k8s pod-log discovery
  (`/var/log/pods` + the kubelet API; needs a ServiceAccount + read RBAC).
- **node-exporter** (DaemonSet, hostPath `/proc` `/sys` `/`), **postgres-exporter**
  (Deployment, `DATA_SOURCE_NAME` → the in-cluster Postgres Service),
  **uptime-kuma** (Deployment + PVC).
- **Grafana** (Deployment) — the existing provisioning under
  [`deploy/grafana/provisioning`](../deploy/grafana/provisioning/)
  (`datasources/`, the five `dashboards/`, `alerting/` with the Telegram contact
  point) is mounted as ConfigMaps; **datasource URLs change** from
  `http://victoriametrics:8428` / `http://loki:3100` to the in-cluster Service
  DNS. Create the dashboard ConfigMap from the canonical files
  (`kubectl -n monitoring create configmap grafana-dashboards --from-file=deploy/grafana/provisioning/dashboards`)
  so they are not duplicated into the chart.

**Loki-push retarget.** The nightly trend pipelines push to Loki via
SSH→`docker run --network …_monitoring curl http://loki:3100` —
[`scripts/mutation-loki-push.sh`](../scripts/mutation-loki-push.sh),
[`scripts/pmat-loki-push.sh`](../scripts/pmat-loki-push.sh),
[`scripts/terraform-drift-loki-push.sh`](../scripts/terraform-drift-loki-push.sh).
At cutover these switch to a throwaway in-cluster curl pod resolving the Loki
Service DNS:

```sh
kubectl -n monitoring run loki-push-$RANDOM --rm -i --restart=Never \
  --image=curlimages/curl:8.11.1 -- \
  curl -sS --fail --max-time 30 -X POST \
  http://opengate-loki.monitoring.svc:3100/loki/api/v1/push \
  -H 'Content-Type: application/json' --data-binary @-
```

The workflows that call them (`mutation.yml`, `pmat-trend.yml`,
`terraform-drift.yml`) gain the kubeconfig setup from step 5.

## 5. Cut CD over to the cluster

Swap the compose deploy in [`cd.yml`](../.github/workflows/cd.yml) for Helm
(this is the cutover-gated change):

- New composite `.github/actions/oci-kube-setup` (mirrors
  [`oci-ssh-setup`](../.github/actions/oci-ssh-setup/action.yml)): OCI auth →
  `oci ce cluster create-kubeconfig` → `kubectl`/`helm` ready.
- `deploy-staging` / `deploy-production`: replace `scp` + `deploy.sh` with
  `helm upgrade --install …`. Helm is idempotent, so the ADR-025 sentinel /
  digest-skip dance is retired (or replaced with `helm diff`).
- **Staging-E2E steps that break under k8s** (see also
  [Continuous Deployment](./Continuous-Deployment.md)):
  - DB reset `docker exec opengate-postgres-staging psql …` →
    `kubectl -n opengate-staging exec sts/opengate-staging-postgres -- psql …`.
  - Playwright SSH tunnel `ssh -fN -L 18080` → run against the staging **ingress
    hostname**, or `kubectl port-forward`.
  - `smoke-test.sh --host 127.0.0.1 --port 18080` → the staging ingress URL
    (the script gains a URL mode).
- **Unchanged** (ephemeral-CI compose, tests the *app* not the deployment):
  `ci.yml` integration/e2e jobs, `e2e-cross-browser.yml`, `load-test.yml`.

## 6. DNS cutover + decommission

1. Point the `A`/`AAAA` records at the ingress-nginx external IP (cert-manager
   issues the TLS cert once DNS resolves).
2. Smoke-test the cluster through real DNS.
3. Stop the compose stack on the VM (`docker compose down`, app **and**
   monitoring) once traffic is fully served by the cluster.
4. Reclaim the freed budget: grow the node pool toward the full 4 OCPU / 24 GB,
   then (PR-C/PR-E) enable Redis Sentinel + ≥2 server replicas.

## Rollback

Until step 6.3, the compose stack is intact — repoint DNS back to the VM and
`docker compose up -d`. After decommission, rollback means re-applying the
compose stack from git + restoring the latest `pg_dump`.
