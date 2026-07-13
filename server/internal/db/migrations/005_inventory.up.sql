-- Edge Sentinel auto-discovery: per-device inventory footprint in Postgres RLS.
-- One row per discovered component (a listening port, host service, DB engine,
-- container, or installed package). Descriptive/relational data only — never a
-- VictoriaMetrics label and never a connection string or credential.

CREATE TABLE IF NOT EXISTS device_inventory (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id       UUID NOT NULL REFERENCES organizations(id),
    device_id    UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('port', 'service', 'db_engine', 'container', 'package')),
    name         TEXT NOT NULL DEFAULT '',
    version      TEXT NOT NULL DEFAULT '',
    port         INTEGER NOT NULL DEFAULT 0 CHECK (port >= 0 AND port <= 65535),
    proto        TEXT NOT NULL DEFAULT '',
    state        TEXT NOT NULL DEFAULT '',
    runtime      TEXT NOT NULL DEFAULT '',
    image        TEXT NOT NULL DEFAULT '',
    first_seen   TIMESTAMPTZ NOT NULL,
    last_seen    TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, device_id, kind, name, port, proto)
);

CREATE INDEX IF NOT EXISTS idx_device_inventory_org_device_kind ON device_inventory(org_id, device_id, kind);

ALTER TABLE device_inventory ENABLE ROW LEVEL SECURITY;
ALTER TABLE device_inventory FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_device_inventory ON device_inventory
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);
