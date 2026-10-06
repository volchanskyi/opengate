-- System tables outside RLS with no foreign key to organizations, so the deny-list and the
-- completion record outlive an organization's data.

-- deleted_ids is the tombstone deny-list, written before any store is touched so write paths
-- reject a purged subject; rows keep ids and scope only, never telemetry.
CREATE TABLE IF NOT EXISTS deleted_ids (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id     UUID NOT NULL,
    device_id  UUID,                                    -- NULL => whole-org tombstone
    scope      TEXT NOT NULL CHECK (scope IN ('device', 'org')),
    deleted_by UUID,                                    -- requesting user; NULL for system sweeps
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One tombstone per device and one per org; the org tombstone supersedes at ingest.
CREATE UNIQUE INDEX IF NOT EXISTS uq_deleted_ids_device
    ON deleted_ids (org_id, device_id) WHERE device_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_deleted_ids_org
    ON deleted_ids (org_id) WHERE device_id IS NULL;

-- purge_jobs persists per-subject progress so a purge resumes after a crash; verified gates
-- completion because VM delete-series reports empty only after background merges.
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
