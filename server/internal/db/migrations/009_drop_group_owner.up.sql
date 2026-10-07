-- Organization scopes visibility and the admin flag gates mutation, so a group has no owner.
-- Groups keep org_id, which scopes them and which the RLS policy enforces.
DROP INDEX IF EXISTS idx_groups_org_id_owner_id;

ALTER TABLE groups_ DROP COLUMN IF EXISTS owner_id;
