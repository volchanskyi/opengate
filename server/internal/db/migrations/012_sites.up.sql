-- A site is a location inside one customer: device groups become sites and gain the customer.
-- security_groups are user permission groups, a separate concept this migration leaves alone.

ALTER TABLE groups_ RENAME TO sites;
ALTER INDEX groups__pkey RENAME TO sites_pkey;
ALTER TABLE sites RENAME CONSTRAINT groups_tenant_id_fkey TO sites_tenant_id_fkey;
ALTER POLICY tenant_isolation_groups ON sites RENAME TO tenant_isolation_sites;

-- Existing sites take the tenant's own customer, the one every device already belongs to.
ALTER TABLE sites ADD COLUMN IF NOT EXISTS organization_id UUID;

UPDATE sites s
   SET organization_id = (
         SELECT o.id FROM organizations o
          WHERE o.tenant_id = s.tenant_id
          ORDER BY o.created_at, o.id
          LIMIT 1)
 WHERE s.organization_id IS NULL;

ALTER TABLE sites ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE sites ADD CONSTRAINT sites_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE;

-- A site name is unique within its customer, as "Head Office" differs for each customer.
ALTER TABLE sites ADD CONSTRAINT sites_organization_id_name_key UNIQUE (organization_id, name);

-- The target half of the composite key on devices below.
ALTER TABLE sites ADD CONSTRAINT sites_organization_id_id_key UNIQUE (organization_id, id);

CREATE INDEX IF NOT EXISTS idx_sites_tenant_id_organization_id ON sites(tenant_id, organization_id);

ALTER TABLE devices RENAME COLUMN group_id TO site_id;
ALTER INDEX IF EXISTS idx_devices_group_id RENAME TO idx_devices_site_id;
ALTER INDEX IF EXISTS idx_devices_tenant_id_group_id RENAME TO idx_devices_tenant_id_site_id;

-- Devices filed into another customer's site are cleared before the constraint lands.
UPDATE devices d
   SET site_id = NULL
 WHERE d.site_id IS NOT NULL
   AND NOT EXISTS (
     SELECT 1 FROM sites s
      WHERE s.id = d.site_id AND s.organization_id = d.organization_id);

-- The pair key lets a device name only a site in its own customer; a null site_id leaves it
-- unchecked, and deleting a site clears only site_id.
ALTER TABLE devices DROP CONSTRAINT devices_group_id_fkey;
ALTER TABLE devices ADD CONSTRAINT devices_site_in_organization_fkey
    FOREIGN KEY (organization_id, site_id) REFERENCES sites(organization_id, id)
    ON DELETE SET NULL (site_id);
