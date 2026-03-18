-- Revert: make devices.group_id NOT NULL again.
-- Devices with NULL group_id will be deleted.

PRAGMA foreign_keys = OFF;

DELETE FROM devices WHERE group_id IS NULL;

CREATE TABLE devices_old (
    id TEXT PRIMARY KEY,
    group_id TEXT NOT NULL REFERENCES groups_(id) ON DELETE CASCADE,
    hostname TEXT NOT NULL DEFAULT '',
    os TEXT NOT NULL DEFAULT '',
    agent_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'offline',
    last_seen TEXT NOT NULL DEFAULT (datetime('now')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO devices_old SELECT * FROM devices;
DROP TABLE devices;
ALTER TABLE devices_old RENAME TO devices;

CREATE INDEX IF NOT EXISTS idx_devices_group_id ON devices(group_id);
CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status);

PRAGMA foreign_keys = ON;
