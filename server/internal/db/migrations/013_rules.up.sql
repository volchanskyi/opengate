-- What a customer has changed about a rule, and which machines a rule cannot be
-- evaluated on.
--
-- A rule's definition is not here. Definitions are versioned YAML compiled into
-- the server, so a predicate is cost-bounded in CI before it reaches an
-- endpoint. What a database has to hold is the part that changes without a
-- deploy: a customer's retuned numbers, a rule's rollout state, and the standing
-- fact that some machines cannot evaluate a rule at all.
--
-- The isolation wall stays at the tenant, like every other tenant-scoped table.
-- organization_id is the customer inside it, and every index leads with the
-- tenant so a scoped read never scans another one's rows.

-- The tenant test all three policies below apply, written once. Stating it in
-- one place is what keeps three tables' isolation identical: an edit to the rule
-- cannot reach two of them and miss the third.
CREATE OR REPLACE FUNCTION app_tenant_visible(row_tenant_id UUID)
RETURNS BOOLEAN
LANGUAGE sql
STABLE
AS $$
    SELECT row_tenant_id = current_setting('app.current_tenant')::uuid
        OR current_setting('app.is_admin', true)::boolean
$$;

-- The target half of the composite keys below. Naming a customer that belongs
-- to a different tenant is the mismatch these tables have to refuse, and the
-- database refuses it rather than a check the application has to remember to
-- run — the same shape migration 012 gave a device and its site.
CREATE UNIQUE INDEX IF NOT EXISTS organizations_tenant_id_id_key
    ON organizations(tenant_id, id);

-- A customer's parameter overrides ---------------------------------------
-- Keyed down the tenancy ladder: a value can be set on one machine, a site, or
-- the whole customer, and the narrower one wins. params carries only numbers the
-- rule declared tunable, validated against that rule's own bounds on write.
CREATE TABLE IF NOT EXISTS rule_bindings (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    rule_id         TEXT NOT NULL CHECK (rule_id <> '' AND length(rule_id) <= 64),
    level           TEXT NOT NULL CHECK (level IN ('device', 'site', 'organization', 'tenant')),
    level_key       UUID NOT NULL,
    -- A bounded tag predicate picking out machines across the level, stored as
    -- an object so a selector is always a set of exact matches and never a
    -- language. '{}' is the level's blanket binding.
    selector        JSONB NOT NULL DEFAULT '{}'::jsonb
                    CHECK (jsonb_typeof(selector) = 'object'),
    -- Breaks a tie between two selectors that both match one machine at one
    -- rung. Set by the operator, because the alternative is a tie-break nobody
    -- can see.
    precedence      INTEGER NOT NULL DEFAULT 0,
    params          JSONB NOT NULL DEFAULT '{}'::jsonb
                    CHECK (jsonb_typeof(params) = 'object'),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by      TEXT NOT NULL DEFAULT '',
    UNIQUE (organization_id, rule_id, level, level_key, selector),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

-- Two selectors at one rung with one precedence would make resolution depend on
-- whatever order the rows came back in. The pair simply cannot be stored: the
-- blanket binding is excluded, since it is already ordered behind every
-- targeted one.
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

-- A rule's rollout state --------------------------------------------------
-- A customer with no row here has not configured the rule, which is not the
-- same as having switched it off; the shipped default applies. kill is separate
-- from enabled on purpose: switching a rule off is an ordinary choice, a kill is
-- an intervention, and the two have to be distinguishable afterwards.
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

-- Which machines a rule cannot be evaluated on ----------------------------
-- Only the durable third of coverage is here. Whether a machine is currently
-- evaluating a rule, and whether it has been heard from at all, are liveness:
-- they are supposed to reset when the server loses sight of the fleet, and a
-- stored 'active' would let a machine unplugged three weeks ago keep claiming it
-- is being watched. Those two stay in memory.
--
-- Being unable to evaluate a rule is a different kind of fact. A containerized
-- agent can never read the kernel's per-host pressure accounting, so that is a
-- standing hole in an estate's monitoring that belongs on a remediation list —
-- and it must answer the same on Tuesday as it did on Monday, whatever the
-- server did in between.
--
-- The presence of a row is the state, so there is no column that can go stale: a
-- machine that starts evaluating the rule has its row deleted rather than
-- flipped. That also keeps steady state at zero writes — a write happens on a
-- change, never on a summary.
CREATE TABLE IF NOT EXISTS rule_coverage_unsupported (
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    -- Deleting a machine takes its coverage with it, or a decommissioned
    -- container inflates the unsupported count forever.
    device_id       UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    rule_id         TEXT NOT NULL CHECK (rule_id <> '' AND length(rule_id) <= 64),
    -- What makes "this estate has been blind to io-stalled since March"
    -- answerable at all.
    since           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (device_id, rule_id),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_rule_coverage_unsupported_tenant_id_organization_id
    ON rule_coverage_unsupported(tenant_id, organization_id);
-- The read behind a coverage summary: how many machines a customer's rule
-- cannot be evaluated on.
CREATE INDEX IF NOT EXISTS idx_rule_coverage_unsupported_organization_id_rule_id
    ON rule_coverage_unsupported(organization_id, rule_id);

ALTER TABLE rule_coverage_unsupported ENABLE ROW LEVEL SECURITY;
ALTER TABLE rule_coverage_unsupported FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_rule_coverage_unsupported ON rule_coverage_unsupported
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));
