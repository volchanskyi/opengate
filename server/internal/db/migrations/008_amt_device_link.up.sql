-- Intel AMT becomes a property of the managed device it belongs to.
--
-- The join key is the SMBIOS system UUID: the agent reads it out of DMI and, on
-- vPro hardware, the AMT firmware presents that same value over CIRA. So a CIRA
-- connection can resolve its device — and through it the owning organization —
-- even though the connection carries no request tenant to inherit. The column is
-- a join key only and is never returned by the API.
--
-- amt_available / amt_version come from the agent's Management Engine reading;
-- amt_model / amt_firmware come from the server's WSMAN query over the CIRA
-- connection. Two writers share the row, so every upsert is column-targeted.
ALTER TABLE device_hardware
    ADD COLUMN IF NOT EXISTS system_uuid   UUID,
    ADD COLUMN IF NOT EXISTS amt_available BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS amt_version   TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS amt_model     TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS amt_firmware  TEXT    NOT NULL DEFAULT '';

-- Serves the CIRA-connect lookup, which searches by system UUID alone across
-- organizations because it runs before any tenant is known.
CREATE INDEX IF NOT EXISTS idx_device_hardware_system_uuid
    ON device_hardware (system_uuid) WHERE system_uuid IS NOT NULL;

-- amt_devices is reduced to connection state: the device row owns the hostname
-- and the hardware row owns the machine model and firmware.
ALTER TABLE amt_devices
    ADD COLUMN IF NOT EXISTS device_id UUID REFERENCES devices(id) ON DELETE CASCADE;

-- Link every row that names a device in the same organization, then discard what
-- is left: an AMT connection with no managed device has no organization to live
-- in, and the next CIRA connect recreates the row against the device that claims
-- its system UUID.
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
