-- An organization is one customer inside a tenant, and a device belongs to exactly one.
-- Isolation stays at the tenant, so this table carries the tenant policy and no second wall.

CREATE TABLE IF NOT EXISTS organizations (
    id          UUID PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    -- An archived organization keeps its devices and history but leaves the working set.
    archived_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_organizations_tenant_id_name ON organizations(tenant_id, name);

ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_organizations ON organizations
    USING (tenant_id = current_setting('app.current_tenant')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (tenant_id = current_setting('app.current_tenant')::uuid OR current_setting('app.is_admin', true)::boolean);

-- Every tenant starts with one organization, so a device always has one to belong to.
INSERT INTO organizations (id, tenant_id, name)
SELECT gen_random_uuid(), t.id, 'Default Organization'
  FROM tenants t
ON CONFLICT DO NOTHING;

-- Deleting an organization cascades to its devices and their dependent rows.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS organization_id UUID;

UPDATE devices d
   SET organization_id = o.id
  FROM organizations o
 WHERE o.tenant_id = d.tenant_id
   AND o.name = 'Default Organization'
   AND d.organization_id IS NULL;

ALTER TABLE devices ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE devices ADD CONSTRAINT devices_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_devices_tenant_id_organization_id
    ON devices(tenant_id, organization_id);
