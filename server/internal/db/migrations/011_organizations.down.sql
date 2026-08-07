-- Reverse of the organization entity.

DROP INDEX IF EXISTS idx_devices_tenant_id_organization_id;
ALTER TABLE devices DROP CONSTRAINT IF EXISTS devices_organization_id_fkey;
ALTER TABLE devices DROP COLUMN IF EXISTS organization_id;

DROP POLICY IF EXISTS tenant_isolation_organizations ON organizations;
DROP INDEX IF EXISTS idx_organizations_tenant_id_name;
DROP TABLE IF EXISTS organizations;
