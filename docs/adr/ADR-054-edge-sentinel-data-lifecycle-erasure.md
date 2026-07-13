---
adr: 054
title: Edge Sentinel Data Lifecycle — Right-to-be-Forgotten Erasure
status: Accepted
date: 2026-07-13
---

# ADR-054: Edge Sentinel Data Lifecycle — Right-to-be-Forgotten Erasure

## Status

Accepted.

## Context

Edge-Sentinel spreads a device's telemetry across several stores: numeric
series and inline anomaly scores in VictoriaMetrics, descriptive process and
inventory rows in Postgres RLS tables, and a durable local store on the agent
itself. Deleting a device or offboarding a tenant must erase all of it — a
right-to-be-forgotten guarantee — without leaving a path for a live stream, an
in-flight backfill, or a misbehaving agent to re-create the data. The stores are
not one transaction, VictoriaMetrics' delete-series is asynchronous, and an
offline agent cannot be wiped until it reconnects, so a naive "delete the row"
is neither complete nor race-safe.

## Decision

- **Immediate hard purge, no undo.** No soft-delete, grace window, or
  export-before-delete. A device delete is a synchronous tracked purge; a
  tenant/fleet delete is an asynchronous resumable job. Both are idempotent.
- **Tombstone-first ordering.** A purge records the subject in a persisted,
  non-RLS `deleted_ids` deny-list **before** touching any store, then deletes
  VictoriaMetrics series, optional cold-tier objects, and the Postgres rows
  **last** — so labels and FKs survive while the stores drain and a crash leaves
  the subject marked deleted, not half-alive.
- **Every write path checks the deny-list.** The agent server warms an in-memory
  copy at startup and rejects a tombstoned device on connect and on every
  write-path control message; a tenant purge records a per-device tombstone for
  each device so the check needs only the device id even after the org linkage is
  gone.
- **Verified, resumable jobs.** `purge_jobs` persists per-store progress;
  completion is gated on a post-delete VictoriaMetrics emptiness check, and an
  interrupted job resumes idempotently at startup. A periodic reconciliation
  sweep garbage-collects orphaned series as defense in depth.
- **Retain the erasure proof.** Audit events and the organization row are
  retained: the audit trail is the compliance record, and referential integrity
  against retained audit events would otherwise block deleting the org row.
- **Server-side only.** The VictoriaMetrics delete key (`-deleteAuthKey`) and any
  object-store credentials never reach the edge; the delete selector always pins
  `org_id` so a purge can never span tenants.

## Consequences

VictoriaMetrics frees disk only on a later merge, so the operator-visible state
machine distinguishes logical completion (ingest blocked, no longer queryable)
from physical/edge completion. Postgres `pg_dump` backups cannot be surgically
edited, so a subject is fully erased only once every backup containing it ages
out under the bucket's Object Storage lifecycle policy — a documented SLA, not a
gap. When numeric telemetry (VictoriaMetrics) is disabled, device deletion falls
back to the plain Postgres delete.

See [Data Lifecycle](../Data-Lifecycle.md), the engine in
[`server/internal/lifecycle`](../../server/internal/lifecycle/), and migration
[`006_data_lifecycle`](../../server/internal/db/migrations/006_data_lifecycle.up.sql).
