# ADR-035: OKE free-tier block-volume remediation (450 GB → 200 GB)

**Status:** Accepted
**Date:** 2026-06-10
**Extends:** [ADR-030](ADR-030-kubernetes-adoption-oke-helm.md) (the OKE/Helm
substrate, the in-cluster Postgres StatefulSet + PVC, the `pg_dump` backup CronJob,
and the in-cluster monitoring stack) and
[ADR-034](ADR-034-scale-out-keda-shared-keys.md) (the `sharedKeys` `/data` emptyDir
switch this ADR generalises to a `persistent` flag). Nothing in those ADRs is
reversed — this records the storage-footprint decisions taken once the cutover was
complete.

## Context

The compose→OKE cutover and the compose-VM decommission left the cluster carrying
**450 GB of OCI block storage** against the **200 GB Always-Free cap**, so ~250 GB
billed (~$6–8/mo). OCI block volumes have a **50 GB minimum**, so the only lever is
the **count** of volumes, not their size. The cutover provisioned nine 50 GB
volumes (eight PVCs + the 50 GB node boot volume):

| PVC | Plan |
|---|---|
| opengate / postgres data | **keep** — prod data, never touched |
| monitoring / victoriametrics | **keep** — metrics TSDB |
| monitoring / loki | **keep** — log store |
| monitoring / uptime-kuma | drop — move uptime off-cluster |
| monitoring / grafana | drop — config is all provisioned, state is disposable |
| opengate-staging / postgres data | drop — ephemeral deploy-test DB |
| opengate-staging / server `/data` | drop — staging CA/keys reset is fine |
| opengate / postgres-backups | drop — move backups off-cluster |

Target: **3 block PVCs (150 GB) + node boot (50 GB) = 200 GB**, exactly at the cap.

## Decision

**1. Uptime monitoring → external SaaS; remove `uptime-kuma` entirely.** The
in-cluster Deployment + PVC + its public `status.<domain>` ingress are deleted from
the `deploy/helm/monitoring` chart. Uptime is now probed by an external SaaS
(UptimeRobot / Better Stack) hitting `https://<domain>/healthz` from off-cluster —
which additionally catches whole-node outages an in-cluster monitor is blind to.
The monitoring chart now exposes **no ingress** (Grafana was already internal,
reached via `kubectl port-forward` / the OCI Bastion).

**2. Grafana `/var/lib/grafana` → `emptyDir`.** Datasources, the five dashboards,
and alerting are provisioned from ConfigMaps and the admin password re-seeds from
the Secret on every start, so a restart loses only UI-created annotations and
alert-state history — not configuration. The 50 GB PVC is not worth that.

**3. Staging Postgres + server `/data` → `emptyDir` via a `persistent` flag.** New
`postgres.storage.persistent` and `server.dataVolume.persistent` values (both
default **true**) swap the respective RWO volume for an `emptyDir` when false.
`values-staging.yaml` sets both false; `values-production.yaml` keeps the defaults,
so **production storage is unchanged**. Staging is a throwaway deploy-test
environment — E2E/smoke seed their own fixtures and re-enroll against the fresh CA
each run, so the per-restart reset is acceptable. (Production's server `/data` is
already an `emptyDir` via ADR-034 `sharedKeys`, independent of this flag.)

**4. Postgres backups → OCI Object Storage via a write-only PAR.** The backup
CronJob no longer writes to a PVC. It runs in two stages sharing an `emptyDir`: an
**init container** (the postgres image — has `pg_dump`, no `curl`) dumps + gzips the
database, then a **main container** (`curlimages/curl` — has `curl`, no `pg_dump`)
`PUT`s the gzip to OCI Object Storage. The destination is a **write-only**
pre-authenticated request (PAR) URL held in the existing `Secret`
(`BACKUP_PAR_URL`) — `AnyObjectWrite`, no read/list, so a leaked token cannot
exfiltrate existing backups and no OCI credentials live in-cluster. Retention moves
from the job's `find -mtime` to an Object Storage **lifecycle policy** on the
bucket. Off-cluster backups also survive total cluster loss, which the PVC never
did.

The two-image split is forced by the images: `postgres:17-alpine` carries `pg_dump`
but only busybox `wget` (no HTTP `PUT`), and a single-image solution would mean
either baking a custom image or `apk add`-ing into a `readOnlyRootFilesystem`
non-root container. An init container (runs to completion before the main
container) is the standard, image-version-independent pattern.

## Consequences

- **Block-volume tally is exactly at the cap:** prod Postgres (50) + VictoriaMetrics
  (50) + Loki (50) + node boot (50) = **200 GB**, zero billable overage. Verified by
  rendering each overlay under `make lint-k8s` and counting `PersistentVolumeClaim`
  docs + StatefulSet `volumeClaimTemplates`.
- **Production data and storage are untouched.** Only staging / Grafana / kuma
  disposable state is discarded; backups *improve* (off-cluster, survive cluster
  loss).
- **New manual provisioning, recorded in the chart `NOTES.txt`:** create the SaaS
  monitors + alert contact, create the OCI bucket + write-only PAR + retention
  lifecycle policy, and add `BACKUP_PAR_URL` to the Secret. Retiring or CNAME-ing
  `status.<domain>` in Cloudflare is a one-off DNS step.
- **Reclaim is a one-time operation:** after `helm upgrade`, the five freed PVCs are
  `kubectl delete`d (StatefulSet PVCs are retained, not auto-removed) so the CSI
  `reclaimPolicy: Delete` deletes the underlying OCI volumes — guarded by a
  verify-before-delete check that the prod-postgres PVC is never in the delete set.
- **A restart now loses staging DB state, the staging CA, and Grafana annotations.**
  All three were judged acceptable above; if a future need makes any of them durable
  again, flip the corresponding `persistent` flag (or re-add a PVC) — the levers are
  values, not code.
