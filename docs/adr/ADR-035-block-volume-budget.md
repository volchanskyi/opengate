---
number: 35
title: The cluster fits the 200 GB storage cap
---

# ADR-035 — The cluster fits the 200 GB storage cap

## Context

The free allowance is 200 GB of block storage across the whole tenancy, and the
binding constraint is the number of volumes, not their size — each one is
rounded up to a minimum.

## Decision

**Uptime checking moves to an external service**, and its in-cluster deployment
goes entirely.

**Grafana's storage is ephemeral.** Datasources, dashboards and alert rules are
provisioned from the chart, so there is nothing in that volume worth keeping.

**Staging's database and server scratch space are ephemeral**, behind a flag, so
production keeps its volume and staging costs none. A staging deploy starts from
a migrated, seeded database.

**Database backups go to OCI Object Storage** through a write-only pre-authenticated
address. An init container dumps and compresses, and the upload container sends
it — split because the image that can dump cannot upload and the image that can
upload cannot dump.

**The node's boot volume holds the container images, and they age out.** The
kubelet removes an image unused for a week, and starts removing unused images at
80% used, down to 75%
([`node-init.sh`](../../deploy/terraform/modules/oke/node-init.sh)). Its
defaults are off for the age and 85% → 80% for the clean-up. Left at those
defaults, the disk fills with every server release and every replaced monitoring
image, and the clean-up starts at the same 85% as eviction: CRI-O keeps its image store
on the root disk, so the kubelet's image-store eviction line (`imagefs.available`
under 15%) is the root disk too. The clean-up moves below that line, as
[kubernetes#124868](https://github.com/kubernetes/kubernetes/issues/124868) asks
of the defaults; the eviction lines stay at the defaults Amazon EKS and Google
GKE run. Moving eviction to 90% instead would need every eviction line written
out, since a settings file naming one drops the defaults of the rest.

**The disk alerts sit on those lines.** The warning fires above the clean-up line
when the disk is heading past eviction within a day at the pace of the last six
hours, the form of the Prometheus node rules; the critical fires two points
before eviction ([`alert-rules.yml`](../../deploy/grafana/provisioning/alerting/alert-rules.yml)).
A fixed percentage below the clean-up line fires on images building up as
designed and never clears.

## Consequences

Volume count is a design constraint anywhere in this chart. Adding a persistent
volume means removing one.

The boot volume carries no alert of its own below the clean-up line; growth there
is the kubelet's to reclaim.
