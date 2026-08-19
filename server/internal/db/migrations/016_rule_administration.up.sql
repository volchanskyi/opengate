-- What an operator can change about a curated rule, and the cross-cutting
-- labels a rule can be aimed at.
--
-- Everything here exists because the alternative was a database console. A
-- threshold tuned wrong, a rule degrading an estate, and an alert budget set
-- from an estimate are all answered by a person changing a number, and none of
-- them can wait for a release.
--
-- The isolation wall stays at the tenant, like every other tenant-scoped table.
-- organization_id is the customer inside it, and every index leads with the
-- tenant so a scoped read never scans another one's rows.

-- The labels a customer's machines can be picked out by ------------------
-- A label is a flat key and value chosen from a list the customer maintains,
-- and it cuts across the tenancy ladder rather than sitting on a rung of it:
-- `role=file-server` describes machines in four offices, which is exactly the
-- set a threshold is usually meant for and exactly the set no rung names.
--
-- The list is a table rather than free text on the assignment because a
-- targeting dimension whose values are typed in is a dimension where
-- `production`, `Production` and `prod` are three estates.
CREATE TABLE IF NOT EXISTS device_tag_labels (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    key             TEXT NOT NULL CHECK (key <> '' AND length(key) <= 64),
    value           TEXT NOT NULL CHECK (value <> '' AND length(value) <= 128),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      TEXT NOT NULL DEFAULT '',
    UNIQUE (organization_id, key, value),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_device_tag_labels_tenant_id_organization_id
    ON device_tag_labels(tenant_id, organization_id);

ALTER TABLE device_tag_labels ENABLE ROW LEVEL SECURITY;
ALTER TABLE device_tag_labels FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_device_tag_labels ON device_tag_labels
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- Which machines carry which label ---------------------------------------
-- One value per key per machine, because a selector asks whether the machine's
-- `role` is `file-server` and a machine holding two roles would answer both. The
-- primary key is what makes that unanswerable rather than a rule the
-- application has to remember.
--
-- The label is referenced rather than copied, so deleting a label from the list
-- cannot leave machines carrying a value the list no longer offers. Deleting is
-- refused while a rule aims at the label; that check reads the selectors and so
-- lives in the application, and RESTRICT here is what stops a delete that
-- somehow got past it from silently widening a threshold across an estate.
CREATE TABLE IF NOT EXISTS device_tags (
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    device_id       UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    label_id        UUID NOT NULL REFERENCES device_tag_labels(id) ON DELETE RESTRICT,
    key             TEXT NOT NULL CHECK (key <> '' AND length(key) <= 64),
    value           TEXT NOT NULL CHECK (value <> '' AND length(value) <= 128),
    assigned_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    assigned_by     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, key),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_device_tags_tenant_id_organization_id
    ON device_tags(tenant_id, organization_id);
-- The read every agent connection makes: this machine's labels, so a targeted
-- binding can be matched against them.
CREATE INDEX IF NOT EXISTS idx_device_tags_device_id
    ON device_tags(device_id);
-- The read behind "which machines carry this label", which is what a delete has
-- to answer and what bulk assignment shows.
CREATE INDEX IF NOT EXISTS idx_device_tags_label_id
    ON device_tags(label_id);

ALTER TABLE device_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE device_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_device_tags ON device_tags
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- A customer's alert budget ----------------------------------------------
-- Both ceilings were chosen from an estimate of a rate nobody had measured, so
-- both have to be movable without a release. The hard maximum each may be
-- raised to is a constant in code rather than a column: a limit stored beside
-- the value it limits is a limit an operator can raise, which is not a limit.
--
-- A customer with no row here is on the shipped budget, which is not the same as
-- a budget of zero.
CREATE TABLE IF NOT EXISTS organization_alert_limits (
    tenant_id             UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id       UUID NOT NULL,
    -- Alerts this customer may store in a rolling hour, across every machine.
    hourly_ceiling        INTEGER NOT NULL CHECK (hourly_ceiling > 0),
    -- Alerts one of this customer's machines may raise in a rolling hour. It is
    -- enforced on the machine, so it travels down with the rules.
    device_hourly_ceiling INTEGER NOT NULL CHECK (device_hourly_ceiling > 0),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by            TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (organization_id),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_organization_alert_limits_tenant_id_organization_id
    ON organization_alert_limits(tenant_id, organization_id);

ALTER TABLE organization_alert_limits ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_alert_limits FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_organization_alert_limits ON organization_alert_limits
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- How a rule is allowed to spread ----------------------------------------
-- The populations each stage reaches and how long a stage is held before it may
-- advance. An estate of twelve machines and an estate of five thousand do not
-- want the same first stage, and an hour is the wrong hold for a rule whose
-- symptom takes a working day to appear.
--
-- There is deliberately no column for switching the automatic pull-back off. It
-- is the mitigation for the one thing here that can degrade an estate at once,
-- so it is not configuration.
ALTER TABLE rule_rollout
    ADD COLUMN IF NOT EXISTS canary_percent INTEGER NOT NULL DEFAULT 1
        CHECK (canary_percent BETWEEN 1 AND 99),
    ADD COLUMN IF NOT EXISTS staged_percent INTEGER NOT NULL DEFAULT 10
        CHECK (staged_percent BETWEEN 1 AND 99),
    ADD COLUMN IF NOT EXISTS canary_hold_secs INTEGER NOT NULL DEFAULT 3600
        CHECK (canary_hold_secs BETWEEN 60 AND 2592000),
    ADD COLUMN IF NOT EXISTS staged_hold_secs INTEGER NOT NULL DEFAULT 21600
        CHECK (staged_hold_secs BETWEEN 60 AND 2592000);

-- A tuned value a new rule version no longer allows ----------------------
-- A rule upgrade keeps the customer's tuning, which means a version that
-- narrows a range inherits a value outside it. The value is moved to the nearest
-- one the new version allows and the move is recorded here until somebody
-- acknowledges it — dropping it would take the customer's decision away
-- silently, and leaving it would put a value on the wire the rule's author
-- refused.
--
-- Keyed on the binding, the parameter and the version that narrowed it, so
-- re-reading the same upgrade records the same single row rather than one per
-- read.
CREATE TABLE IF NOT EXISTS rule_binding_clamps (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    binding_id      UUID NOT NULL REFERENCES rule_bindings(id) ON DELETE CASCADE,
    rule_id         TEXT NOT NULL CHECK (rule_id <> '' AND length(rule_id) <= 64),
    rule_version    INTEGER NOT NULL,
    param           TEXT NOT NULL CHECK (param <> '' AND length(param) <= 64),
    -- What the customer had set, and what the new version moved it to.
    from_value      DOUBLE PRECISION NOT NULL,
    to_value        DOUBLE PRECISION NOT NULL,
    clamped_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at TIMESTAMPTZ,
    acknowledged_by TEXT NOT NULL DEFAULT '',
    UNIQUE (binding_id, param, rule_version),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_rule_binding_clamps_tenant_id_organization_id
    ON rule_binding_clamps(tenant_id, organization_id);
-- The read behind the flag on a rule page: what this customer has not
-- acknowledged for this rule.
CREATE INDEX IF NOT EXISTS idx_rule_binding_clamps_organization_id_rule_id
    ON rule_binding_clamps(organization_id, rule_id)
    WHERE acknowledged_at IS NULL;

ALTER TABLE rule_binding_clamps ENABLE ROW LEVEL SECURITY;
ALTER TABLE rule_binding_clamps FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_rule_binding_clamps ON rule_binding_clamps
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- The read behind the noise badge: how many alerts one customer's rule has
-- raised recently. The badge is drawn for every rule in the pack on one screen,
-- so it is one grouped read over a bounded window rather than a read per rule.
CREATE INDEX IF NOT EXISTS idx_alerts_organization_id_received_at_rule_id
    ON alerts(organization_id, received_at DESC, rule_id);
