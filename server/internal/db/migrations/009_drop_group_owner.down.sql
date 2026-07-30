-- Dropping a column is lossy: the owner each group carried is gone, so the
-- restored column is nullable. A NOT NULL restore would need values that no
-- longer exist anywhere in the database.
ALTER TABLE groups_ ADD COLUMN IF NOT EXISTS owner_id UUID REFERENCES users(id);

CREATE INDEX IF NOT EXISTS idx_groups_org_id_owner_id ON groups_(org_id, owner_id);
