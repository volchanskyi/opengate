CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL DEFAULT '',
    display_name  TEXT NOT NULL DEFAULT '',
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The trailing underscore avoids the reserved word GROUP.
CREATE TABLE IF NOT EXISTS groups_ (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL,
    owner_id   UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS devices (
    id            UUID PRIMARY KEY,
    group_id      UUID REFERENCES groups_(id) ON DELETE SET NULL,
    hostname      TEXT NOT NULL DEFAULT '',
    os            TEXT NOT NULL DEFAULT '',
    os_display    TEXT NOT NULL DEFAULT '',
    agent_version TEXT NOT NULL DEFAULT '',
    capabilities  JSONB NOT NULL DEFAULT '[]'::jsonb,
    status        TEXT NOT NULL DEFAULT 'offline',
    last_seen     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_devices_group_id ON devices(group_id);
CREATE INDEX IF NOT EXISTS idx_devices_status   ON devices(status);

CREATE TABLE IF NOT EXISTS agent_sessions (
    token      TEXT PRIMARY KEY,
    device_id  UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_device_id ON agent_sessions(device_id);

CREATE TABLE IF NOT EXISTS web_push_subscriptions (
    endpoint TEXT PRIMARY KEY,
    user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    p256dh   TEXT NOT NULL DEFAULT '',
    auth     TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS audit_events (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    UUID NOT NULL,
    action     TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    details    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_audit_events_user_id    ON audit_events(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events(created_at DESC);

CREATE TABLE IF NOT EXISTS amt_devices (
    uuid      UUID PRIMARY KEY,
    hostname  TEXT NOT NULL DEFAULT '',
    model     TEXT NOT NULL DEFAULT '',
    firmware  TEXT NOT NULL DEFAULT '',
    status    TEXT NOT NULL DEFAULT 'offline',
    last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_amt_devices_status ON amt_devices(status);

CREATE TABLE IF NOT EXISTS enrollment_tokens (
    id         UUID PRIMARY KEY,
    token      TEXT NOT NULL UNIQUE,
    label      TEXT NOT NULL DEFAULT '',
    created_by UUID NOT NULL REFERENCES users(id),
    max_uses   INTEGER NOT NULL DEFAULT 0,
    use_count  INTEGER NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_token ON enrollment_tokens(token);

CREATE TABLE IF NOT EXISTS security_groups (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS security_group_members (
    group_id UUID NOT NULL REFERENCES security_groups(id) ON DELETE CASCADE,
    user_id  UUID NOT NULL REFERENCES users(id)          ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_sgm_user_id ON security_group_members(user_id);

INSERT INTO security_groups (id, name, description, is_system)
VALUES ('00000000-0000-0000-0000-000000000001', 'Administrators', 'Full system access', TRUE)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS device_updates (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    version   TEXT NOT NULL,
    status    TEXT NOT NULL DEFAULT 'pending',
    error     TEXT NOT NULL DEFAULT '',
    pushed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acked_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_device_updates_device  ON device_updates(device_id);
CREATE INDEX IF NOT EXISTS idx_device_updates_version ON device_updates(version);

CREATE TABLE IF NOT EXISTS device_hardware (
    device_id          UUID PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    cpu_model          TEXT NOT NULL DEFAULT '',
    cpu_cores          INTEGER NOT NULL DEFAULT 0,
    ram_total_mb       BIGINT NOT NULL DEFAULT 0,
    disk_total_mb      BIGINT NOT NULL DEFAULT 0,
    disk_free_mb       BIGINT NOT NULL DEFAULT 0,
    network_interfaces JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS device_logs (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    device_id  UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp  TEXT NOT NULL,
    level      TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    message    TEXT NOT NULL DEFAULT '',
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_device_logs_device_id ON device_logs(device_id);
CREATE INDEX IF NOT EXISTS idx_device_logs_level     ON device_logs(device_id, level);
CREATE INDEX IF NOT EXISTS idx_device_logs_timestamp ON device_logs(device_id, timestamp);
