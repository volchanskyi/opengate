-- The restored column is nullable because the dropped owner values cannot be recovered.
ALTER TABLE groups_ ADD COLUMN IF NOT EXISTS owner_id UUID REFERENCES users(id);

CREATE INDEX IF NOT EXISTS idx_groups_org_id_owner_id ON groups_(org_id, owner_id);
