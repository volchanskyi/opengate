DROP INDEX IF EXISTS idx_organizations_one_default;
ALTER TABLE organizations DROP COLUMN IF EXISTS is_default;
