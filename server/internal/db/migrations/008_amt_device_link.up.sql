-- The SMBIOS system UUID is a join key only, resolving a CIRA connection to its device and org.
-- Two writers share the row (the agent and the WSMAN query), so every upsert is column-targeted.
ALTER TABLE device_hardware
    ADD COLUMN IF NOT EXISTS system_uuid   UUID,
    ADD COLUMN IF NOT EXISTS amt_available BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS amt_version   TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS amt_model     TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS amt_firmware  TEXT    NOT NULL DEFAULT '';

-- Serves the CIRA-connect lookup by system UUID across organizations, before any tenant is known.
CREATE INDEX IF NOT EXISTS idx_device_hardware_system_uuid
    ON device_hardware (system_uuid) WHERE system_uuid IS NOT NULL;

-- amt_devices holds connection state only; the device and hardware rows own the rest.
ALTER TABLE amt_devices
    ADD COLUMN IF NOT EXISTS device_id UUID REFERENCES devices(id) ON DELETE CASCADE;

-- Rows matching no device in their organization are discarded; the next CIRA connect
-- recreates them against the device that claims the system UUID.
UPDATE amt_devices a
   SET device_id = d.id
  FROM devices d
 WHERE a.device_id IS NULL
   AND a.hostname <> ''
   AND d.org_id = a.org_id
   AND d.hostname = a.hostname;

DELETE FROM amt_devices WHERE device_id IS NULL;

ALTER TABLE amt_devices ALTER COLUMN device_id SET NOT NULL;

-- One AMT connection per device keeps the device read a plain primary-key join.
ALTER TABLE amt_devices ADD CONSTRAINT amt_devices_device_id_key UNIQUE (device_id);

ALTER TABLE amt_devices
    DROP COLUMN IF EXISTS hostname,
    DROP COLUMN IF EXISTS model,
    DROP COLUMN IF EXISTS firmware;
