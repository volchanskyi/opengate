ALTER TABLE amt_devices
    ADD COLUMN IF NOT EXISTS hostname TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS model    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS firmware TEXT NOT NULL DEFAULT '';

-- Restore the hostname the device row carried, so a rolled-back deployment finds
-- the join key its code expects.
UPDATE amt_devices a
   SET hostname = d.hostname
  FROM devices d
 WHERE d.id = a.device_id;

ALTER TABLE amt_devices DROP CONSTRAINT IF EXISTS amt_devices_device_id_key;
ALTER TABLE amt_devices DROP COLUMN IF EXISTS device_id;

DROP INDEX IF EXISTS idx_device_hardware_system_uuid;
ALTER TABLE device_hardware
    DROP COLUMN IF EXISTS amt_firmware,
    DROP COLUMN IF EXISTS amt_model,
    DROP COLUMN IF EXISTS amt_version,
    DROP COLUMN IF EXISTS amt_available,
    DROP COLUMN IF EXISTS system_uuid;
