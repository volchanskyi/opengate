-- The customer an MSP serves. A tenant is the wall the database enforces; an
-- organization is one customer inside it, and a device belongs to exactly one.
-- Structural and for targeting — the isolation boundary stays at the tenant, so
-- this table carries the tenant policy like every other tenant-scoped table and
-- adds no second wall of its own.

CREATE TABLE IF NOT EXISTS organizations (
    id          UUID PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    -- Set when a customer is retired. The rows stay: an archived organization
    -- keeps its devices and its history, and is simply out of the working set.
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

-- Every tenant starts with one organization, so a device always has somewhere
-- to belong and the picker always has something to pick.
INSERT INTO organizations (id, tenant_id, name)
SELECT gen_random_uuid(), t.id, 'Default Organization'
  FROM tenants t
ON CONFLICT DO NOTHING;

-- Deleting an organization takes its devices with it, which in turn cascades
-- their telemetry, inventory, hardware and update rows.
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
