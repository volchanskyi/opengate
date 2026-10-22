-- When a second live machine last presented a device's identity, and the two addresses it was
-- seen from, so the device page can warn while it is recent.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS identity_shared_at TIMESTAMPTZ;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS identity_shared_addresses TEXT[] NOT NULL DEFAULT '{}';
