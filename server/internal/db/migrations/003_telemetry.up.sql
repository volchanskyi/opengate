-- Edge Sentinel telemetry: process snapshots in Postgres RLS.

CREATE TABLE IF NOT EXISTS device_processes (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id       UUID NOT NULL REFERENCES organizations(id),
    device_id    UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ts           TIMESTAMPTZ NOT NULL,
    rank         INTEGER NOT NULL CHECK (rank >= 0),
    basename     TEXT NOT NULL DEFAULT '',
    cmdline_hash TEXT,
    pid          BIGINT NOT NULL CHECK (pid >= 0),
    cpu          DOUBLE PRECISION NOT NULL DEFAULT 0,
    mem          DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, device_id, ts, rank)
);

CREATE INDEX IF NOT EXISTS idx_device_processes_org_id_device_ts ON device_processes(org_id, device_id, ts DESC);

ALTER TABLE device_processes ENABLE ROW LEVEL SECURITY;
ALTER TABLE device_processes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_device_processes ON device_processes
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);
