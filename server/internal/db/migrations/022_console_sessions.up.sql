-- A console sign-in: the browser holds the session id in a cookie and the server keeps only its
-- hash. A system table like device_certificates, read before a tenant is known.
CREATE TABLE IF NOT EXISTS console_sessions (
    id_hash      BYTEA PRIMARY KEY CHECK (octet_length(id_hash) = 32), -- SHA-256 of the session id
    user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    tenant_id    UUID NOT NULL,
    email        TEXT NOT NULL,
    is_admin     BOOLEAN NOT NULL,
    csrf_token   TEXT NOT NULL,
    started_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_console_sessions_user ON console_sessions (user_id);
