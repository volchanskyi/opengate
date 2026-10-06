-- A label is a flat key and value from a list the customer maintains, so spelling variants
-- cannot split one estate.
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

-- The primary key allows one value per key per machine so a selector has one answer; RESTRICT
-- refuses deleting a label that machines still carry.
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
-- The read every agent connection makes: this machine's labels.
CREATE INDEX IF NOT EXISTS idx_device_tags_device_id
    ON device_tags(device_id);
-- The read behind which machines carry a label.
CREATE INDEX IF NOT EXISTS idx_device_tags_label_id
    ON device_tags(label_id);

ALTER TABLE device_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE device_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_device_tags ON device_tags
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- A customer with no row here is on the shipped budget; the hard maximums live in code.
CREATE TABLE IF NOT EXISTS organization_alert_limits (
    tenant_id             UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id       UUID NOT NULL,
    -- Alerts this customer may store in a rolling hour, across every machine.
    hourly_ceiling        INTEGER NOT NULL CHECK (hourly_ceiling > 0),
    -- Alerts one of this customer's machines may raise in a rolling hour, enforced on the machine.
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

-- Stage populations and hold times; the automatic pull-back has no switch.
ALTER TABLE rule_rollout
    ADD COLUMN IF NOT EXISTS canary_percent INTEGER NOT NULL DEFAULT 1
        CHECK (canary_percent BETWEEN 1 AND 99),
    ADD COLUMN IF NOT EXISTS staged_percent INTEGER NOT NULL DEFAULT 10
        CHECK (staged_percent BETWEEN 1 AND 99),
    ADD COLUMN IF NOT EXISTS canary_hold_secs INTEGER NOT NULL DEFAULT 3600
        CHECK (canary_hold_secs BETWEEN 60 AND 2592000),
    ADD COLUMN IF NOT EXISTS staged_hold_secs INTEGER NOT NULL DEFAULT 21600
        CHECK (staged_hold_secs BETWEEN 60 AND 2592000);

-- Records a tuned value moved into the range a new rule version allows, until acknowledged.
-- Keyed on binding, parameter and version so re-reading an upgrade records one row.
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
-- The unacknowledged clamps of one customer's rule.
CREATE INDEX IF NOT EXISTS idx_rule_binding_clamps_organization_id_rule_id
    ON rule_binding_clamps(organization_id, rule_id)
    WHERE acknowledged_at IS NULL;

ALTER TABLE rule_binding_clamps ENABLE ROW LEVEL SECURITY;
ALTER TABLE rule_binding_clamps FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_rule_binding_clamps ON rule_binding_clamps
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- Serves one grouped read of recent alerts per rule for a customer.
CREATE INDEX IF NOT EXISTS idx_alerts_organization_id_received_at_rule_id
    ON alerts(organization_id, received_at DESC, rule_id);
