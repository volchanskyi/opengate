-- security_groups: named permission groups (e.g. "Administrators").
CREATE TABLE IF NOT EXISTS security_groups (
    id         TEXT    PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    description TEXT   NOT NULL DEFAULT '',
    is_system  INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- security_group_members: many-to-many between users and security_groups.
CREATE TABLE IF NOT EXISTS security_group_members (
    group_id TEXT NOT NULL REFERENCES security_groups(id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_sgm_user_id ON security_group_members(user_id);

-- Seed built-in Administrators group with well-known UUID.
INSERT OR IGNORE INTO security_groups (id, name, description, is_system, created_at, updated_at)
VALUES ('00000000-0000-0000-0000-000000000001', 'Administrators', 'Full system access', 1,
        strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));

-- Migrate existing admin users into the Administrators group.
INSERT OR IGNORE INTO security_group_members (group_id, user_id, added_at)
SELECT '00000000-0000-0000-0000-000000000001', id, strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
FROM users WHERE is_admin = 1;
