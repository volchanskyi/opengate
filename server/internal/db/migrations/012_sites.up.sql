-- A site is a location or department inside one customer. Today's device groups
-- are already that level in everything but name and parent, so they take both:
-- the table becomes sites and gains the customer above it.
--
-- security_groups and security_group_members are user permission groups, an
-- unrelated concept that merely shares the word. They are untouched here.

ALTER TABLE groups_ RENAME TO sites;
ALTER INDEX groups__pkey RENAME TO sites_pkey;
ALTER TABLE sites RENAME CONSTRAINT groups_tenant_id_fkey TO sites_tenant_id_fkey;
ALTER POLICY tenant_isolation_groups ON sites RENAME TO tenant_isolation_sites;

-- The customer above the site. Existing sites take the tenant's own customer —
-- the same one migration 011 gave every device — so nothing is orphaned.
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

-- "Head Office" names a different building for each customer, so a site name is
-- unique within its customer rather than across the tenant.
ALTER TABLE sites ADD CONSTRAINT sites_organization_id_name_key UNIQUE (organization_id, name);

-- The target half of the composite key on devices below.
ALTER TABLE sites ADD CONSTRAINT sites_organization_id_id_key UNIQUE (organization_id, id);

CREATE INDEX IF NOT EXISTS idx_sites_tenant_id_organization_id ON sites(tenant_id, organization_id);

-- Devices ---------------------------------------------------------------
ALTER TABLE devices RENAME COLUMN group_id TO site_id;
ALTER INDEX IF EXISTS idx_devices_group_id RENAME TO idx_devices_site_id;
ALTER INDEX IF EXISTS idx_devices_tenant_id_group_id RENAME TO idx_devices_tenant_id_site_id;

-- A device filed into a site whose customer is not the device's own is the
-- mismatch this level has to refuse. Clear those before the constraint lands,
-- so the pair is consistent by the time the database starts enforcing it.
UPDATE devices d
   SET site_id = NULL
 WHERE d.site_id IS NOT NULL
   AND NOT EXISTS (
     SELECT 1 FROM sites s
      WHERE s.id = d.site_id AND s.organization_id = d.organization_id);

-- The single-column key becomes a pair, so a device can name only a site inside
-- its own customer — the mismatch is refused by the database rather than by a
-- check the application has to remember to run. site_id stays nullable, since
-- an unfiled machine is normal, and a null referencing column leaves the pair
-- unchecked, which is exactly the wanted behaviour. Deleting a site unfiles its
-- machines rather than taking them with it, so only site_id is cleared.
ALTER TABLE devices DROP CONSTRAINT devices_group_id_fkey;
ALTER TABLE devices ADD CONSTRAINT devices_site_in_organization_fkey
    FOREIGN KEY (organization_id, site_id) REFERENCES sites(organization_id, id)
    ON DELETE SET NULL (site_id);
