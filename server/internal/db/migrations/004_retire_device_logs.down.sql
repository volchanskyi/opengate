-- Recreate the device-log cache in its post-multitenancy shape (org_id + RLS),
-- so a further rollback to the pre-tenancy schema can strip org_id cleanly.
CREATE TABLE IF NOT EXISTS device_logs (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id     UUID NOT NULL REFERENCES organizations(id),
    device_id  UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp  TEXT NOT NULL,
    level      TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    message    TEXT NOT NULL DEFAULT '',
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_device_logs_device_id ON device_logs(device_id);
CREATE INDEX IF NOT EXISTS idx_device_logs_level     ON device_logs(device_id, level);
CREATE INDEX IF NOT EXISTS idx_device_logs_timestamp ON device_logs(device_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_device_logs_org_id_device_id ON device_logs(org_id, device_id);
CREATE INDEX IF NOT EXISTS idx_device_logs_org_id_level ON device_logs(org_id, device_id, level);
CREATE INDEX IF NOT EXISTS idx_device_logs_org_id_timestamp ON device_logs(org_id, device_id, timestamp);

ALTER TABLE device_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE device_logs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_device_logs ON device_logs
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);
