# Reclaim OKE block volumes — execute the ADR-035 storage cut (450 → 200 GB)

**Status:** Executed 2026-06-11 — cluster now at 200 GB (3 block volumes), verified
end-to-end. Only the external uptime SaaS + Cloudflare DNS remain (user-owned),
tracked in [`techdebt.md`](../../techdebt.md). Archived 2026-06-11.

## Context

The ADR-035 chart changes are merged (commit `f8ea7f1`), but the **running cluster
still bills the overage** — the code change alone does not free volumes. The live
cluster carries **8 × 50 GB block PVCs + 50 GB node boot = 450 GB** against the
**200 GB OCI Always-Free cap** (~$6–8/mo billed). This runbook performs the
one-time operator reclaim to bring it to exactly 200 GB.

Verified reachable from this workstation: `kubectl` (context `…c23expbbogq`, 1 OKE
node), `helm` v3.16, `oci` CLI v3.75 **authenticated** (OS namespace `axcrowpqlsio`).

**User decisions (2026-06-11):** remove `uptime-kuma` now (accept a brief uptime-
monitoring gap until the external SaaS is set up); **do not** preserve old local
backup dumps (the new CronJob starts fresh in the bucket).

### Live PVC inventory (the decision matrix)

| Namespace | PVC | Kind of claim | Action |
|---|---|---|---|
| opengate | `data-opengate-postgres-0` | STS volumeClaimTemplate | **KEEP** — prod data, never touched |
| monitoring | `storage-monitoring-victoriametrics-0` | STS volumeClaimTemplate | **KEEP** — metrics |
| monitoring | `data-monitoring-loki-0` | STS volumeClaimTemplate | **KEEP** — logs |
| monitoring | `monitoring-uptime-kuma` | chart-template PVC (helm-managed) | delete — auto by `helm upgrade` |
| monitoring | `monitoring-grafana` | chart-template PVC (helm-managed) | delete — auto by `helm upgrade` |
| opengate | `opengate-postgres-backups` | chart-template PVC (helm-managed) | delete — auto by `helm upgrade` |
| opengate-staging | `opengate-staging-server-data` | chart-template PVC (helm-managed) | delete — auto by `helm upgrade` |
| opengate-staging | `data-opengate-staging-postgres-0` | STS volumeClaimTemplate | delete — **manual** (see §3) |

`oci-bv` storageClass = `reclaimPolicy: Delete` → deleting a PVC deletes the
underlying OCI block volume. Final state: 3 block PVCs (150 GB) + boot (50) = 200 GB.

### Key operational facts that shape the procedure

1. **Helm deletes chart-template PVCs** removed from the manifest between revisions
   (no `helm.sh/resource-policy: keep` on ours). So `helm upgrade` reclaims the 4
   helm-managed PVCs (kuma, grafana, backups, staging-server-data) **automatically**.
2. **StatefulSet `volumeClaimTemplates` are immutable**, and STS-created PVCs are
   **not** helm-managed. The only STS converting to emptyDir is **staging postgres**
   — it needs the STS deleted-and-recreated, then its orphaned PVC deleted by hand.
   Prod/VM/Loki STSes are unchanged, so they have no immutability issue.
3. Manual `helm upgrade` must **replicate CD's invocation** (preserve `image.tag`,
   `domain`, etc.) — otherwise it redeploys chart defaults. Prod is live on
   `image.tag=sha-a96765f`, `domain=opengate.cloudisland.net`,
   `certManager.email=ivan.volchanskyi@gmail.com`, `sharedKeys.enabled=true`.

## Discovered constants

- Region `us-sanjose-1` · OS namespace `axcrowpqlsio` · AD `US-SANJOSE-1-AD-1`
- Compartment (tenancy root, where the cluster lives):
  `ocid1.tenancy.oc1..aaaaaaaambbkhvmwwpkznitzgcbg24q3wr3xg26ija5vaxnwdsq54xbskvpa`
- Releases: `monitoring`/ns `monitoring`, `opengate`/ns `opengate`,
  `opengate-staging`/ns `opengate-staging`
- Prod postgres pod `opengate-postgres-0`; bucket name `opengate-pg-backups`

## Execution

Run from `/home/ivan/opengate`. **Pre-flight:** snapshot state and capture live
values so each `helm upgrade` reproduces the deployed config:

```bash
kubectl get pvc -A > /tmp/reclaim-pvc-before.txt          # audit trail
helm get values monitoring        -n monitoring        -o yaml > /tmp/mon-values.yaml
helm get values opengate-staging  -n opengate-staging  -o yaml > /tmp/stg-values.yaml   # read image.tag + domain
helm get values opengate          -n opengate          -o yaml > /tmp/prod-values.yaml  # sanity check
```

### 1. OCI Object Storage backup target (bucket + write-only PAR + lifecycle)

```bash
NS=axcrowpqlsio
COMP=ocid1.tenancy.oc1..aaaaaaaambbkhvmwwpkznitzgcbg24q3wr3xg26ija5vaxnwdsq54xbskvpa
oci os bucket create --namespace "$NS" --compartment-id "$COMP" --name opengate-pg-backups

# Write-only PAR (AnyObjectWrite, no read/list) — 1-year expiry; capture the URL.
ACCESS_URI=$(oci os preauth-request create --namespace "$NS" \
  --bucket-name opengate-pg-backups --name pg-backup-writer \
  --access-type AnyObjectWrite --time-expires 2027-06-11T00:00:00Z \
  --query 'data."access-uri"' --raw-output)
PAR_URL="https://objectstorage.us-sanjose-1.oraclecloud.com${ACCESS_URI}"   # ends in /o/

# Retention = delete objects > 7 days (replaces the old find -mtime).
oci os object-lifecycle-policy put --namespace "$NS" --bucket-name opengate-pg-backups --force \
  --items '[{"name":"expire-old","action":"DELETE","timeAmount":7,"timeUnit":"DAYS","isEnabled":true,"objectNameFilter":{"inclusionPrefixes":["opengate-"]}}]'
```

### 2. Add `BACKUP_PAR_URL` to the prod Secret

```bash
kubectl patch secret opengate-secrets -n opengate --type merge \
  -p "{\"data\":{\"BACKUP_PAR_URL\":\"$(printf %s "$PAR_URL" | base64 -w0)\"}}"
```

### 3. Helm upgrades (each reclaims its helm-managed PVCs)

**Monitoring** (removes kuma Deploy/Svc/PVC + status Ingress; grafana PVC→emptyDir):
```bash
helm upgrade monitoring deploy/helm/monitoring -n monitoring -f /tmp/mon-values.yaml --wait --timeout 5m
```

**Production** (new backup CronJob; removes backups PVC; prod postgres STS untouched):
```bash
helm upgrade --install opengate deploy/helm/opengate -n opengate \
  -f deploy/helm/opengate/values-production.yaml \
  --set image.tag=sha-a96765f \
  --set server.replicas=1 \
  --set domain=opengate.cloudisland.net \
  --set certManager.email=ivan.volchanskyi@gmail.com \
  --wait --timeout 5m
```

**Staging** — STS immutability: delete the postgres STS first, then upgrade, then
delete the orphaned STS PVC (substitute the live `image.tag`/`domain` from
`/tmp/stg-values.yaml`):
```bash
kubectl delete statefulset opengate-staging-postgres -n opengate-staging --cascade=foreground
helm upgrade --install opengate-staging deploy/helm/opengate -n opengate-staging \
  -f deploy/helm/opengate/values-staging.yaml \
  --set image.tag=<from /tmp/stg-values.yaml> \
  --set domain=<from /tmp/stg-values.yaml> \
  --set certManager.create=false \
  --wait --timeout 5m
kubectl delete pvc data-opengate-staging-postgres-0 -n opengate-staging
```

### 4. Delete any straggler freed PVCs (guarded)

Helm should have removed the 4 chart-template PVCs; sweep for any that survived,
**explicitly excluding the 3 keepers**:
```bash
for pvc in monitoring/monitoring-uptime-kuma monitoring/monitoring-grafana \
           opengate/opengate-postgres-backups opengate-staging/opengate-staging-server-data; do
  ns=${pvc%/*}; name=${pvc#*/}
  kubectl get pvc "$name" -n "$ns" 2>/dev/null && kubectl delete pvc "$name" -n "$ns"
done
# NEVER delete: opengate/data-opengate-postgres-0, monitoring/{storage-…-victoriametrics-0,data-…-loki-0}
```

## Verification

```bash
# 1. Exactly the 3 keepers remain bound.
kubectl get pvc -A      # expect: data-opengate-postgres-0, storage-…-victoriametrics-0, data-…-loki-0
# 2. OCI shows 3 block volumes (resolve the full AD name first).
AD=$(oci iam availability-domain list --compartment-id "$COMP" --query 'data[0].name' --raw-output)
oci bv volume list --compartment-id "$COMP" --availability-domain "$AD" \
  --lifecycle-state AVAILABLE --query 'length(data)'   # expect 3
# 3. App + Grafana healthy; prod data PVC stayed Bound the whole time.
curl -fsS https://opengate.cloudisland.net/healthz          # → ok
make tunnel &  curl -sf http://localhost:3000/api/health    # Grafana up (dashboards re-provisioned)
# 4. New backup path works — trigger the CronJob once, confirm an object lands.
kubectl create job -n opengate --from=cronjob/opengate-postgres-backup manual-verify-$(date +%s)
kubectl logs -n opengate -l app.kubernetes.io/component=postgres-backup --tail=20
oci os object list --namespace axcrowpqlsio --bucket-name opengate-pg-backups --query 'data[].name'
```

## Risk / rollback

- **Prod data is never touched** — `data-opengate-postgres-0` is a STS
  volumeClaimTemplate left unchanged by the prod upgrade; the §4 sweep and the
  verify step both assert it stays Bound. Deletion is guarded by an explicit
  allowlist (never the 3 keepers).
- **Irreversible:** PVC deletion → volume deletion. Staging DB/CA, Grafana
  annotations, and old local backup dumps are discarded (all pre-approved).
- A mid-run helm upgrade that fails leaves the release on the prior revision
  (`helm rollback <rel>` restores it); the OCI bucket/PAR steps are independent and
  idempotent. The next CD deploy re-applies the same (merged) chart — no divergence.

## User-owned follow-ups (external accounts — I cannot do these)

- **External uptime SaaS:** create UptimeRobot/Better Stack monitors on
  `https://opengate.cloudisland.net/healthz` (+ optional TCP QUIC 9090 / MPS 4433),
  point the alert contact at the existing Telegram/email, enable the status page.
- **Cloudflare DNS:** retire `status.opengate.cloudisland.net` or CNAME it to the
  SaaS status page.

On completion, close the "ADR-035 block-volume remediation — manual reclaim" item
in [`techdebt.md`](techdebt.md).
