-- Rule definitions are versioned YAML in the server; these tables hold what changes without a
-- deploy. Isolation is the tenant, and every table carries the tenant policy.

-- The tenant test all three policies apply, written once so the tables' isolation stays identical.
CREATE OR REPLACE FUNCTION app_tenant_visible(row_tenant_id UUID)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT row_tenant_id = current_setting('app.current_tenant')::uuid
        OR current_setting('app.is_admin', true)::boolean
$$;

-- The target half of the composite keys below: the database refuses a customer of another tenant.
CREATE UNIQUE INDEX IF NOT EXISTS organizations_tenant_id_id_key
    ON organizations(tenant_id, id);

-- Overrides are keyed down the tenancy ladder (machine, site, customer, tenant); narrower wins.
-- params holds only numbers the rule declares tunable, validated against its bounds on write.
CREATE TABLE IF NOT EXISTS rule_bindings (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    rule_id         TEXT NOT NULL CHECK (rule_id <> '' AND length(rule_id) <= 64),
    level           TEXT NOT NULL CHECK (level IN ('device', 'site', 'organization', 'tenant')),
    level_key       UUID NOT NULL,
    -- A bounded tag predicate of exact matches, never a language; '{}' is the blanket binding.
    selector        JSONB NOT NULL DEFAULT '{}'::jsonb
                    CHECK (jsonb_typeof(selector) = 'object'),
    -- Operator-set tie-break between two selectors that match one machine at one rung.
    precedence      INTEGER NOT NULL DEFAULT 0,
    params          JSONB NOT NULL DEFAULT '{}'::jsonb
                    CHECK (jsonb_typeof(params) = 'object'),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by      TEXT NOT NULL DEFAULT '',
    UNIQUE (organization_id, rule_id, level, level_key, selector),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

-- Two selectors at one rung with one precedence would make resolution depend on row order, so
-- that pair cannot be stored; the blanket binding is excluded as it orders behind targeted ones.
CREATE UNIQUE INDEX IF NOT EXISTS uq_rule_bindings_selector_precedence
    ON rule_bindings(organization_id, rule_id, level, level_key, precedence)
    WHERE selector <> '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_rule_bindings_tenant_id_organization_id
    ON rule_bindings(tenant_id, organization_id);
-- The read the agent connection makes: every binding for one customer's rule.
CREATE INDEX IF NOT EXISTS idx_rule_bindings_organization_id_rule_id
    ON rule_bindings(organization_id, rule_id);

ALTER TABLE rule_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE rule_bindings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_rule_bindings ON rule_bindings
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- No row means the customer has not configured the rule, so the shipped default applies. kill is
-- separate from enabled so an intervention stays distinguishable from an ordinary off switch.
CREATE TABLE IF NOT EXISTS rule_rollout (
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id  UUID NOT NULL,
    rule_id          TEXT NOT NULL CHECK (rule_id <> '' AND length(rule_id) <= 64),
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    canary_group     TEXT NOT NULL DEFAULT '' CHECK (length(canary_group) <= 64),
    rollout_percent  INTEGER NOT NULL DEFAULT 100
                     CHECK (rollout_percent BETWEEN 0 AND 100),
    kill             BOOLEAN NOT NULL DEFAULT FALSE,
    stage_entered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by       TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (organization_id, rule_id),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_rule_rollout_tenant_id_organization_id
    ON rule_rollout(tenant_id, organization_id);

ALTER TABLE rule_rollout ENABLE ROW LEVEL SECURITY;
ALTER TABLE rule_rollout FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_rule_rollout ON rule_rollout
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- Only durable coverage gaps are stored; liveness resets with the server's view, in memory.
-- A row is the state, so none goes stale: a machine that starts evaluating has its row deleted.
CREATE TABLE IF NOT EXISTS rule_coverage_unsupported (
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    -- Deleting a machine deletes its coverage, keeping the unsupported count accurate.
    device_id       UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    rule_id         TEXT NOT NULL CHECK (rule_id <> '' AND length(rule_id) <= 64),
    -- When the machine first became unable to evaluate the rule.
    since           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (device_id, rule_id),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_rule_coverage_unsupported_tenant_id_organization_id
    ON rule_coverage_unsupported(tenant_id, organization_id);
-- The read behind a coverage summary: machines a customer's rule cannot be evaluated on.
CREATE INDEX IF NOT EXISTS idx_rule_coverage_unsupported_organization_id_rule_id
    ON rule_coverage_unsupported(organization_id, rule_id);

ALTER TABLE rule_coverage_unsupported ENABLE ROW LEVEL SECURITY;
ALTER TABLE rule_coverage_unsupported FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_rule_coverage_unsupported ON rule_coverage_unsupported
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));
