CREATE TABLE IF NOT EXISTS device_hardware (
    device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    cpu_model TEXT NOT NULL DEFAULT '',
    cpu_cores INTEGER NOT NULL DEFAULT 0,
    ram_total_mb INTEGER NOT NULL DEFAULT 0,
    disk_total_mb INTEGER NOT NULL DEFAULT 0,
    disk_free_mb INTEGER NOT NULL DEFAULT 0,
    network_interfaces TEXT NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
