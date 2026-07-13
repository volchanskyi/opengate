-- Edge Sentinel data lifecycle: right-to-be-forgotten cascading erasure.
--
-- Two system-level tables owned by the server-side purge orchestrator. Neither
-- is tenant-scoped (RLS) and neither carries a foreign key to organizations:
-- both must outlive an organization's own data so the deny-list keeps rejecting
-- a purged subject and the completion record survives as the erasure proof.

-- deleted_ids is the persisted tombstone / deny-list. Every purge records the
-- deleted device-id (or org-id) here FIRST, before touching any store, so every
-- write path can reject a tombstoned subject: no live stream, in-flight
-- backfill, or misbehaving agent can re-create purged data. Rows are retained
-- indefinitely and carry ids plus purge scope only — never telemetry.
CREATE TABLE IF NOT EXISTS deleted_ids (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id     UUID NOT NULL,
    device_id  UUID,                                    -- NULL => whole-org tombstone
    scope      TEXT NOT NULL CHECK (scope IN ('device', 'org')),
    deleted_by UUID,                                    -- requesting user; NULL for system sweeps
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One tombstone per device and one per org. A device tombstone and its owning
-- org tombstone may coexist; the org tombstone supersedes at ingest.
CREATE UNIQUE INDEX IF NOT EXISTS uq_deleted_ids_device
    ON deleted_ids (org_id, device_id) WHERE device_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_deleted_ids_org
    ON deleted_ids (org_id) WHERE device_id IS NULL;

-- purge_jobs persists the orchestrator's progress per subject so a purge is
-- idempotent and resumes after a server crash. Per-store flags record which
-- stores are already erased; verified gates completion (VM delete-series is
-- async and only reports empty once background merges drop the series).
CREATE TABLE IF NOT EXISTS purge_jobs (
    id             UUID PRIMARY KEY,
    org_id         UUID NOT NULL,
    device_id      UUID,                                -- NULL => tenant/org-wide purge
    scope          TEXT NOT NULL CHECK (scope IN ('device', 'org')),
    state          TEXT NOT NULL,
    vm_deleted     BOOLEAN NOT NULL DEFAULT FALSE,
    object_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    pg_deleted     BOOLEAN NOT NULL DEFAULT FALSE,
    verified       BOOLEAN NOT NULL DEFAULT FALSE,
    requested_by   UUID,
    last_error     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at   TIMESTAMPTZ
);

-- Fast lookup of the jobs a restarting server must resume.
CREATE INDEX IF NOT EXISTS idx_purge_jobs_incomplete
    ON purge_jobs (created_at) WHERE completed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_purge_jobs_org ON purge_jobs (org_id, created_at DESC);
