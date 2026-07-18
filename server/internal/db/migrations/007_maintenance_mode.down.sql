DROP INDEX IF EXISTS idx_devices_maintenance;
ALTER TABLE devices
    DROP COLUMN IF EXISTS maintenance_reason,
    DROP COLUMN IF EXISTS maintenance_by,
    DROP COLUMN IF EXISTS maintenance_since,
    DROP COLUMN IF EXISTS maintenance_on;
