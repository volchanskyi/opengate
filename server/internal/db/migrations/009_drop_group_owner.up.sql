-- Organization is the visibility boundary and the admin flag is the mutation
-- boundary, so a group has no owner to consult: every member of an organization
-- sees every group in it, and only an administrator creates or deletes one.
--
-- The column and the index built on it are dropped together. Groups keep org_id,
-- which is what scopes them and what the RLS policy enforces.
DROP INDEX IF EXISTS idx_groups_org_id_owner_id;

ALTER TABLE groups_ DROP COLUMN IF EXISTS owner_id;
