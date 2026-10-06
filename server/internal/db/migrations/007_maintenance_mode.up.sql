-- Server-authoritative per-device maintenance state, pushed to the agent, which suppresses
-- telemetry and alerting while maintenance_on is set. maintenance_by has no foreign key.
ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS maintenance_on     BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS maintenance_since  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS maintenance_by     UUID,
    ADD COLUMN IF NOT EXISTS maintenance_reason TEXT        NOT NULL DEFAULT '';

-- Partial index keyed by org, so the maintenance fleet count reads only suppressed devices.
CREATE INDEX IF NOT EXISTS idx_devices_maintenance
    ON devices (org_id) WHERE maintenance_on;
