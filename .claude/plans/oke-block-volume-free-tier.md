# OKE block-volume free-tier remediation (450 GB → ≤200 GB)

## Context

The compose→OKE cutover (and the VM decommission, commit `be98b5e`) left the
cluster's **block storage at 450 GB against the 200 GB OCI Always-Free cap →
~250 GB billable (~$6–8/mo)**. The decommission itself was clean (VM instance +
boot volume `TERMINATED`, old bastion `DELETED`, no orphaned volumes/IPs/backups
/LBs — audited 2026-06-10). The overage is purely the cutover's PVC footprint:

| | PVC | GB | plan |
|---|---|---|---|
| keep | opengate / data-opengate-postgres-0 | 50 | **block (prod data)** |
| keep | monitoring / storage-victoriametrics-0 | 50 | **block (metrics)** |
| keep | monitoring / data-loki-0 | 50 | **block (logs)** |
| move | monitoring / uptime-kuma | 50 | → external SaaS (remove pod) |
| move | monitoring / grafana | 50 | → emptyDir |
| move | opengate-staging / data-opengate-staging-postgres-0 | 50 | → emptyDir |
| move | opengate-staging / opengate-staging-server-data | 50 | → emptyDir |
| move | opengate / opengate-postgres-backups | 50 | → OCI Object Storage |

OCI block volumes have a **50 GB minimum**, so the only lever is **count, not
size**. Target: 3 block PVCs (150 GB) + node boot (50 GB) = **200 GB**, exactly
at the cap. **User decision (2026-06-10): external SaaS for uptime monitoring**
(removes kuma entirely; external vantage also catches whole-node outages an
in-cluster monitor is blind to). Prod Postgres data is **never touched**.

## Phases

### 1. Uptime → external SaaS; remove uptime-kuma (−50 GB, −1 pod)
- **Cluster:** delete [`deploy/helm/monitoring/templates/uptime-kuma.yaml`](../../deploy/helm/monitoring/templates/uptime-kuma.yaml); drop the kuma block from [`values.yaml`](../../deploy/helm/monitoring/values.yaml); drop the `status.*` host from [`templates/ingress.yaml`](../../deploy/helm/monitoring/templates/ingress.yaml); update [`templates/NOTES.txt`](../../deploy/helm/monitoring/templates/NOTES.txt).
- **Manual (user):** create UptimeRobot/Better Stack monitors — HTTPS `https://opengate.cloudisland.net/healthz` (keyword `ok`), optional TCP checks on the node IP for QUIC `9090`/MPS `4433`; alert contact = existing Telegram/email; enable the hosted status page.
- **DNS (Cloudflare):** retire `status.opengate.cloudisland.net` or CNAME it to the SaaS status page.

### 2. grafana → emptyDir (−50 GB)
- [`deploy/helm/monitoring/templates/grafana.yaml`](../../deploy/helm/monitoring/templates/grafana.yaml): swap the PVC volume for `emptyDir`. Safe — dashboards/datasources/alerting are provisioned from ConfigMaps and the admin password re-seeds from the Secret; only annotations/alert-history are lost on restart.

### 3. staging postgres + server-data → emptyDir (−100 GB)
- [`deploy/helm/opengate/values-staging.yaml`](../../deploy/helm/opengate/values-staging.yaml): gate Postgres persistence + server `/data` to `emptyDir` for staging only. `values-production.yaml` is **unchanged** — prod keeps its block volume. Staging DB + CA reset on restart (acceptable for a deploy-test env; e2e/smoke seed their own state).

### 4. postgres-backups → OCI Object Storage (−50 GB)
- Create a bucket (`opengate-pg-backups`) + a long-expiry **write-only pre-authenticated request (PAR)** URL (no in-cluster creds; just a URL Secret) — via `oci os bucket create` + `oci os preauth-request create`.
- Rewrite [`deploy/helm/opengate/templates/postgres-backup-cronjob.yaml`](../../deploy/helm/opengate/templates/postgres-backup-cronjob.yaml) to `curl -X PUT --upload-file` the gzip dump to `<PAR-base>/opengate-<ts>.sql.gz` instead of the `/backups` PVC; drop the PVC + its volume mount. PAR URL added to the existing Secret ([`secrets.example.yaml`](../../deploy/helm/opengate/secrets.example.yaml) documents the new key). Retention via an OS lifecycle policy (delete > keepDays) replacing the `find -mtime`.

### 5. Reclaim + verify
- `helm upgrade monitoring` + `helm upgrade opengate` (prod) + staging.
- **Delete the 5 freed PVCs** (`kubectl delete pvc` — StatefulSet PVCs are retained, not auto-removed) so the CSI `reclaimPolicy: Delete` deletes the underlying OCI block volumes. **Verify each target PVC before deleting** (prod-postgres must NOT be in the delete set).
- Confirm `oci bv volume list` shows **3** volumes (150 GB); free-tier tally = 200 GB.

## Critical files
- `deploy/helm/monitoring/`: `templates/uptime-kuma.yaml` (delete), `templates/grafana.yaml`, `templates/ingress.yaml`, `templates/NOTES.txt`, `values.yaml`
- `deploy/helm/opengate/`: `values-staging.yaml`, `templates/postgres-backup-cronjob.yaml`, `values.yaml`, `secrets.example.yaml`
- `docs/Monitoring.md` (uptime now external; Grafana access), `.claude/techdebt.md` (record/close), `.claude/phases.md`
- Reference pattern: the in-cluster Service URLs + ConfigMap provisioning already in the monitoring chart; the existing pg_dump recipe in the current `postgres-backup-cronjob.yaml`.

## Manual / user steps (gated)
- SaaS account + monitors + alert contact (cluster removal is mine).
- Cloudflare DNS for `status.*`.
- I can create the OCI bucket + PAR via `oci` CLI, or you provide the PAR URL.

## Risk
- **Prod data untouched** (prod-postgres PVC kept). Only staging/grafana/kuma state is discarded (all acceptable); backups *improve* (off-cluster, survive cluster loss).
- Brief pod downtime on grafana/staging during volume swap (downtime pre-approved).
- PVC/volume deletion is irreversible — the verify-before-delete step in §5 is the guard.

## Verification
- `oci bv volume list --compartment-id <tenancy> --availability-domain pQib:US-SANJOSE-1-AD-1` → 3 volumes.
- `kubectl get pvc -A` → only the 3 keepers bound.
- `curl -fsS https://opengate.cloudisland.net/healthz` → `ok`; Grafana loads (dashboards re-provisioned); a Loki + VictoriaMetrics query returns data.
- A manual CronJob run lands `opengate-<ts>.sql.gz` in the bucket (`oci os object list`).
- External SaaS monitor shows green; `status.*` resolves to the SaaS page (or is retired).
- `make lint-k8s` + full gauntlet green; commit.
