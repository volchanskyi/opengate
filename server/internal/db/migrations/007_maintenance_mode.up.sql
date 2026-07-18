-- Maintenance mode: a per-device operational state an administrator flips to
-- quiet a device during disruptive host work (package upgrades, service
-- restarts, reboots). It is the server-authoritative desired state, pushed to
-- the agent over the control channel; the agent suppresses telemetry and
-- alerting while it is set. Default is Active (maintenance_on = FALSE) — the
-- toggle is the exceptional suppression, not a telemetry enable.
--
-- maintenance_since / _by / _reason are populated on entry and cleared on exit.
-- maintenance_by carries the id of the user who last set the state and has no
-- foreign key, matching the deleted_by / requested_by action-attribution
-- columns; the audit log is the authoritative actor record.
ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS maintenance_on     BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS maintenance_since  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS maintenance_by     UUID,
    ADD COLUMN IF NOT EXISTS maintenance_reason TEXT        NOT NULL DEFAULT '';

-- Serves the tenant fleet count of devices currently in maintenance: a partial
-- index keyed by org so the count reads only the (typically small) suppressed
-- set rather than scanning every device.
CREATE INDEX IF NOT EXISTS idx_devices_maintenance
    ON devices (org_id) WHERE maintenance_on;
