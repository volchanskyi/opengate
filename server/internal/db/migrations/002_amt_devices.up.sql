CREATE TABLE IF NOT EXISTS amt_devices (
    uuid TEXT PRIMARY KEY,
    hostname TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    firmware TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'offline',
    last_seen TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_amt_devices_status ON amt_devices(status);
