-- Each tenant marks one customer its default: the one a machine or site naming no customer joins.
-- The mark is set once, so it stays where it is when the server's clock steps back.
ALTER TABLE organizations ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE organizations
   SET is_default = TRUE
 WHERE id IN (SELECT DISTINCT ON (tenant_id) id
                FROM organizations
               ORDER BY tenant_id, created_at, id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_organizations_one_default
    ON organizations (tenant_id) WHERE is_default;
