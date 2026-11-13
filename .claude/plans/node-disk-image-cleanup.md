# The cluster machine's disk fills with images nothing runs

Ivan's decision (modal, 2026-11-13): the read-only look and a one-time prune now, then this plan.
Nothing here is implemented yet; each step waits for Ivan's go-ahead.

## What is wrong

- The alert `disk-usage-warning` fired on 2026-11-12 23:32 UTC: root filesystem 79.1% used.
- Root (`/dev/mapper/ocivolume-root`, xfs) is 38 GB of the 50 GB boot volume; `/var/oled` holds
  10.6 GB with 10.1 GB free.
- 69% on 2026-10-13, a 5-point jump on 10-24/25 (three Grafana images, ~2 GB, during the ARM
  fix), then about 104 MB a day over the last 14 days.
- Pods' own writable layers and logs come to under 0.5 GB (kubelet stats summary).
- Container images take the rest. Of the 50 largest the node reports (10.4 GB), at least ~2.5 GB is
  images nothing runs: `grafana-oss:11.6.5` and `11.6.0` (1.3 GB), `uptime-kuma:1.23.16`
  (0.46 GB), `promtail:3.5.0` and `loki:3.5.0` (0.34 GB), cert-manager `v1.20.2` and `v1.20.4`
  (0.43 GB), old `opengate-server` tags, and older OKE system images (digest-only).
- The kubelet (read from `configz`) removes unused images only above **85%** and down to
  **80%** (`imageGCHighThresholdPercent`, `imageGCLowThresholdPercent`), never by age
  (`imageMaximumGCAge: 0s`), and evicts pods at **90%** (`nodefs.available: 10%`).
- So the 75% warning fires on images building up as designed. Once clean-up starts, the disk
  sits between 80% and 85% and the warning never clears.
- The critical alert at 90% is the moment pods are evicted.
- At today's rate the disk reaches 85% in about three weeks.

## What the industry does

- kube-prometheus moved its disk thresholds to sit with kubelet's image clean-up, and
  node-exporter made them configurable for that reason: `NodeFilesystemSpaceFillingUp` predicts a
  fill (`predict_linear` over 6 h); `NodeFilesystemAlmostOutOfSpace` fires at 5% and 3% left.
- Kubernetes 1.30+ removes images unused for a set time (`imageMaximumGCAge`, KEP-4210, beta and on
  in 1.34, the cluster's version); its own default is 0, off.
- OKE takes kubelet settings through the node pool's start-up script
  (`oke-init.sh --kubelet-extra-args`), applied when a node is created. On a one-node pool that is
  an outage while the node is replaced.

## Now (production, Ivan runs or asks for each command)

1. Look, read-only:
   `kubectl debug node/10.0.2.227 -it --image=docker.io/library/alpine:3.21.9 --profile=sysadmin -- chroot /host sh -c 'du -xh --max-depth=2 / | sort -h | tail -20; crictl images; journalctl --disk-usage'`
   Records here what images, system logs and the OS each hold.
2. Prune unused images once, from the same shell: `crictl rmi --prune`. Then delete the debug pod.
   Usage is read back from `node_filesystem_avail_bytes` and recorded here.

## Steps, each test first

1. **The alert follows the machine's own clean-up.** In
   [`alert-rules.yml`](../../deploy/grafana/provisioning/alerting/alert-rules.yml)
   (`disk-usage-warning`, `disk-usage-critical`): a warning when the disk is predicted to fill
   (the node-mixin form, with a floor that sits above kubelet's 85%), a critical before eviction.
   **Decision for Ivan:** the horizons and floors, brought with the readings from Now.
2. **Images unused for a set time are removed.** The node pool's start-up script in
   [`modules/oke/main.tf`](../../deploy/terraform/modules/oke/main.tf) (`node_metadata.user_data`)
   passes the kubelet its age limit and thresholds; the Terraform test in
   [`free_tier.tftest.hcl`](../../deploy/terraform/modules/oke/tests/free_tier.tftest.hcl) pins
   them. **Decision for Ivan:** the age (a week keeps a rollback's image on the node), and when the
   node is replaced (an OKE node-image upgrade replaces it anyway).
3. The ADRs that describe the node and its storage
   ([ADR-030](../../docs/adr/ADR-030-kubernetes-on-oke.md),
   [ADR-035](../../docs/adr/ADR-035-block-volume-budget.md)) and the monitoring chapter describe
   the above.
4. `/refactor` → commit (the gauntlet) → the post-commit push.

## Done when

- The warning is quiet while images build up below kubelet's clean-up, and fires on a fill the
  clean-up cannot reclaim.
- The node removes images unused past the age Ivan sets.
- This plan is deleted in the commit landing its last step, with its `phases.md` row.
