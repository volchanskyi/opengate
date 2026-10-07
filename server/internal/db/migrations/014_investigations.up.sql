-- Incidents open before the alerts that fold into them; occurrences and device_count are
-- application state the schema cannot keep true.
CREATE TABLE IF NOT EXISTS incidents (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    -- Keyed on the rule id so an open incident survives a rule version upgrade.
    rule_id         TEXT NOT NULL CHECK (rule_id <> '' AND length(rule_id) <= 64),
    -- The organization is the widest scope, so two customers' outages never share an incident.
    scope           TEXT NOT NULL CHECK (scope IN ('device', 'site', 'organization')),
    scope_key       UUID NOT NULL,
    severity        TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    -- An incident in 'new' is the triage queue.
    status          TEXT NOT NULL DEFAULT 'new'
                    CHECK (status IN ('new', 'acknowledged', 'investigating', 'resolved')),
    assignee_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    opened_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Event time, so a retroactive finding folds by when it happened on the machine.
    first_seen      TIMESTAMPTZ NOT NULL,
    last_seen       TIMESTAMPTZ NOT NULL,
    resolved_at     TIMESTAMPTZ,
    -- Required of a person closing an incident, absent when the system closes one.
    cause_code      TEXT CHECK (cause_code IS NULL OR cause_code IN (
                        'resolved_self', 'fixed_by_tech', 'hardware_fault', 'expected_load',
                        'false_positive', 'duplicate', 'wont_fix')),
    occurrences     INTEGER NOT NULL DEFAULT 0 CHECK (occurrences >= 0),
    device_count    INTEGER NOT NULL DEFAULT 0 CHECK (device_count >= 0),
    CHECK (last_seen >= first_seen),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

-- One open incident per grouping key, race-free; resolved incidents sit outside the index
-- so a recurrence opens a new incident.
CREATE UNIQUE INDEX IF NOT EXISTS uq_incidents_open_group
    ON incidents(organization_id, rule_id, scope, scope_key)
    WHERE status <> 'resolved';

CREATE INDEX IF NOT EXISTS idx_incidents_tenant_id_organization_id
    ON incidents(tenant_id, organization_id);
-- The triage queue: one customer's open rooms, newest activity first.
CREATE INDEX IF NOT EXISTS idx_incidents_organization_id_status_last_seen
    ON incidents(organization_id, status, last_seen DESC);

ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_incidents ON incidents
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

CREATE TABLE IF NOT EXISTS alerts (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    -- Erasing a machine erases its alerts and their evidence; incident counts are application state.
    device_id       UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    rule_id         TEXT NOT NULL CHECK (rule_id <> '' AND length(rule_id) <= 64),
    rule_version    INTEGER NOT NULL CHECK (rule_version > 0),
    severity        TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    metric          TEXT NOT NULL DEFAULT '' CHECK (length(metric) <= 64),
    -- Absent for a rule that fires on an event.
    value           DOUBLE PRECISION,
    window_start    TIMESTAMPTZ NOT NULL,
    window_end      TIMESTAMPTZ NOT NULL,
    observed_at     TIMESTAMPTZ NOT NULL,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- A finding from a retroactive scan of local history; it folds by its event time.
    backfilled      BOOLEAN NOT NULL DEFAULT FALSE,
    incident_id     UUID REFERENCES incidents(id) ON DELETE SET NULL,
    -- The device's compressed evidence, frozen at write time and never rewritten.
    evidence        BYTEA,
    evidence_codec  TEXT NOT NULL DEFAULT '' CHECK (length(evidence_codec) <= 32),
    -- The identity a reconnect replay resolves against; a device-chosen id changes when the
    -- agent loses its local store.
    UNIQUE (device_id, rule_id, rule_version, window_start),
    CHECK (window_end >= window_start),
    -- The evidence cap holds on the row itself because evidence is immutable and unfetchable.
    CHECK (evidence IS NULL OR length(evidence) <= 65536),
    -- Evidence needs a named codec to be readable.
    CHECK ((evidence IS NULL) = (evidence_codec = '')),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_alerts_tenant_id_organization_id
    ON alerts(tenant_id, organization_id);
-- The hourly ceiling's read: how much one customer has raised recently.
CREATE INDEX IF NOT EXISTS idx_alerts_organization_id_received_at
    ON alerts(organization_id, received_at DESC);
-- The alerts in an incident, also read to recompute counts after an erasure.
CREATE INDEX IF NOT EXISTS idx_alerts_incident_id ON alerts(incident_id);
CREATE INDEX IF NOT EXISTS idx_alerts_device_id_observed_at
    ON alerts(device_id, observed_at DESC);

ALTER TABLE alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE alerts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_alerts ON alerts
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- Append-only history of how an incident reached its current state.
CREATE TABLE IF NOT EXISTS incident_events (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    incident_id     UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    kind            TEXT NOT NULL CHECK (kind IN (
                        'alert_folded', 'status_change', 'assignment',
                        'comment', 'device_offline', 'resolution')),
    -- Absent when the system acted.
    actor_id        UUID REFERENCES users(id) ON DELETE SET NULL,
    body            JSONB NOT NULL DEFAULT '{}'::jsonb
                    CHECK (jsonb_typeof(body) = 'object'),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_incident_events_tenant_id_organization_id
    ON incident_events(tenant_id, organization_id);
CREATE INDEX IF NOT EXISTS idx_incident_events_incident_id_at
    ON incident_events(incident_id, at);

ALTER TABLE incident_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_incident_events ON incident_events
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));
