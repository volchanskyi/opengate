-- Reverses 012_sites: the level loses its customer and goes back to being a
-- flat filing label on the tenant.

ALTER TABLE devices DROP CONSTRAINT devices_site_in_organization_fkey;
ALTER INDEX IF EXISTS idx_devices_tenant_id_site_id RENAME TO idx_devices_tenant_id_group_id;
ALTER INDEX IF EXISTS idx_devices_site_id RENAME TO idx_devices_group_id;
ALTER TABLE devices RENAME COLUMN site_id TO group_id;

DROP INDEX IF EXISTS idx_sites_tenant_id_organization_id;
ALTER TABLE sites DROP CONSTRAINT sites_organization_id_id_key;
ALTER TABLE sites DROP CONSTRAINT sites_organization_id_name_key;
ALTER TABLE sites DROP CONSTRAINT sites_organization_id_fkey;
ALTER TABLE sites DROP COLUMN IF EXISTS organization_id;

ALTER POLICY tenant_isolation_sites ON sites RENAME TO tenant_isolation_groups;
ALTER TABLE sites RENAME CONSTRAINT sites_tenant_id_fkey TO groups_tenant_id_fkey;
ALTER INDEX sites_pkey RENAME TO groups__pkey;
ALTER TABLE sites RENAME TO groups_;

ALTER TABLE devices ADD CONSTRAINT devices_group_id_fkey
    FOREIGN KEY (group_id) REFERENCES groups_(id) ON DELETE SET NULL;
