-- Make devices.group_id nullable so newly enrolled devices can exist
-- without a group assignment. SQLite requires table recreation for this.

PRAGMA foreign_keys = OFF;

CREATE TABLE devices_new (
    id TEXT PRIMARY KEY,
    group_id TEXT REFERENCES groups_(id) ON DELETE SET NULL,
    hostname TEXT NOT NULL DEFAULT '',
    os TEXT NOT NULL DEFAULT '',
    agent_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'offline',
    last_seen TEXT NOT NULL DEFAULT (datetime('now')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO devices_new SELECT * FROM devices;
DROP TABLE devices;
ALTER TABLE devices_new RENAME TO devices;

CREATE INDEX IF NOT EXISTS idx_devices_group_id ON devices(group_id);
CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status);

PRAGMA foreign_keys = ON;
