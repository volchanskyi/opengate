-- Track per-device update push outcomes.
CREATE TABLE device_updates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error TEXT NOT NULL DEFAULT '',
    pushed_at TEXT NOT NULL DEFAULT (datetime('now')),
    acked_at TEXT
);

CREATE INDEX idx_device_updates_device ON device_updates(device_id);
CREATE INDEX idx_device_updates_version ON device_updates(version);
