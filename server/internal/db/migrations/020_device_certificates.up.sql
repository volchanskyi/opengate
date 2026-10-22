-- Every certificate the server issues, recorded before it is sent: admission refuses a
-- certificate not listed here. A system table like deleted_ids, outside tenant row security.
CREATE TABLE IF NOT EXISTS device_certificates (
    fingerprint   BYTEA PRIMARY KEY CHECK (octet_length(fingerprint) = 32), -- SHA-256 of the leaf
    device_id     UUID NOT NULL,
    issuer_hash   BYTEA NOT NULL CHECK (octet_length(issuer_hash) = 48),  -- SHA-384 of the issuer
    serial        TEXT NOT NULL,
    not_before    TIMESTAMPTZ NOT NULL,
    not_after     TIMESTAMPTZ NOT NULL,
    source        TEXT NOT NULL CHECK (source IN ('enroll', 'renew')),
    issued_at     TIMESTAMPTZ NOT NULL,
    first_seen_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_device_certificates_device
    ON device_certificates (device_id, not_before DESC);
