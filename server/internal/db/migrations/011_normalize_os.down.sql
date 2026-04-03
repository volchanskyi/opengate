-- Restore the pretty OS name from os_display back into os.
UPDATE devices SET os = os_display WHERE os_display != '';

-- SQLite does not support DROP COLUMN before 3.35.0; use a table rebuild.
CREATE TABLE devices_backup AS SELECT
  id, group_id, hostname, os, agent_version, capabilities, status, last_seen, created_at, updated_at
FROM devices;
DROP TABLE devices;
ALTER TABLE devices_backup RENAME TO devices;
