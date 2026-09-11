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

## Consequences

Volume count is a design constraint anywhere in this chart. Adding a persistent
volume means removing one.
