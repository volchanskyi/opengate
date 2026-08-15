-- What a machine reported was wrong, the room that gathers it, and what people
-- did about it.
--
-- An alert is the only carrier of the detail behind a signal. Central keeps one
-- 60 s average per dimension and there is no path for asking the endpoint
-- afterwards, so whatever arrives on the alert is the whole of what will ever be
-- known about that moment. That is why the evidence blob lives on the alert row
-- itself rather than anywhere it could be fetched from later, why it is
-- immutable once written, and why the vocabularies below are closed by check
-- constraint rather than by an application convention: a severity nothing can
-- render or a cause code nothing can report on would be stored happily and
-- discovered by whoever opens the incident.
--
-- The isolation wall stays at the tenant, like every other tenant-scoped table.
-- organization_id is the customer inside it — every grouping key, every read and
-- the hourly alert ceiling all key on the customer, because at the tenant one
-- customer's bad night would merge with another's and consume its budget.

-- The room ------------------------------------------------------------------
-- An incident is where an estate's event is investigated, so it is opened
-- before the alerts that fold into it can point at it. occurrences and
-- device_count are application state: they say how much has folded in and across
-- how many machines, and nothing in the schema can keep them true on its own.
CREATE TABLE IF NOT EXISTS incidents (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    -- Keyed on the rule, never the rule version: upgrading a rule while an
    -- incident is open must not fork the room somebody is working in.
    rule_id         TEXT NOT NULL CHECK (rule_id <> '' AND length(rule_id) <= 64),
    -- How wide the room is. The customer is the broadest there is — folding
    -- across tenants would put two customers' unrelated outages in one room with
    -- no correct assignee.
    scope           TEXT NOT NULL CHECK (scope IN ('device', 'site', 'organization')),
    scope_key       UUID NOT NULL,
    severity        TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    -- An incident in 'new' is the triage queue, which is why no separate
    -- promotion entity exists.
    status          TEXT NOT NULL DEFAULT 'new'
                    CHECK (status IN ('new', 'acknowledged', 'investigating', 'resolved')),
    assignee_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    opened_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Event time, not receipt time: a retroactive finding folds by when it
    -- happened on the machine, or a week-old freeze would sort as today's.
    first_seen      TIMESTAMPTZ NOT NULL,
    last_seen       TIMESTAMPTZ NOT NULL,
    resolved_at     TIMESTAMPTZ,
    -- Required of a person closing an incident, absent when the system closes
    -- one. false_positive is load-bearing: it is the channel that says which
    -- curated rule needs its threshold moved.
    cause_code      TEXT CHECK (cause_code IS NULL OR cause_code IN (
                        'resolved_self', 'fixed_by_tech', 'hardware_fault', 'expected_load',
                        'false_positive', 'duplicate', 'wont_fix')),
    occurrences     INTEGER NOT NULL DEFAULT 0 CHECK (occurrences >= 0),
    device_count    INTEGER NOT NULL DEFAULT 0 CHECK (device_count >= 0),
    CHECK (last_seen >= first_seen),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

-- One open room per grouping key, enforced where a race cannot get past it. Two
-- alerts arriving on two connections at once would otherwise each open their own
-- incident and split one estate-wide event into two rooms nobody can reconcile.
-- Resolved incidents are outside the index, so the same condition recurring next
-- month opens a new room rather than colliding with a closed one.
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

-- What a machine reported ---------------------------------------------------
CREATE TABLE IF NOT EXISTS alerts (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    -- Erasing a machine erases what it reported, evidence included. The counts
    -- on the incident it folded into are application state and do not follow.
    device_id       UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    rule_id         TEXT NOT NULL CHECK (rule_id <> '' AND length(rule_id) <= 64),
    rule_version    INTEGER NOT NULL CHECK (rule_version > 0),
    severity        TEXT NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    metric          TEXT NOT NULL DEFAULT '' CHECK (length(metric) <= 64),
    -- Absent for a rule that fires on an event rather than a reading.
    value           DOUBLE PRECISION,
    window_start    TIMESTAMPTZ NOT NULL,
    window_end      TIMESTAMPTZ NOT NULL,
    observed_at     TIMESTAMPTZ NOT NULL,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- A finding a retroactive scan produced over local history rather than a
    -- live firing. It folds by its real event time all the same.
    backfilled      BOOLEAN NOT NULL DEFAULT FALSE,
    incident_id     UUID REFERENCES incidents(id) ON DELETE SET NULL,
    -- Everything the device knew about why this fired, compressed, frozen at
    -- write time and never rewritten: a rewritten blob would silently stop being
    -- the snapshot the incident was investigated against.
    evidence        BYTEA,
    evidence_codec  TEXT NOT NULL DEFAULT '' CHECK (length(evidence_codec) <= 32),
    -- The identity a reconnect replay resolves against. Deliberately not the id
    -- the device chose: an agent that lost its local store picks a new one and
    -- would duplicate every alert it still had to send.
    UNIQUE (device_id, rule_id, rule_version, window_start),
    CHECK (window_end >= window_start),
    -- The cap is a property of the row, not of the path that wrote it. Evidence
    -- is immutable and unfetchable, so a blob that slipped past an application
    -- check would sit here forever.
    CHECK (evidence IS NULL OR length(evidence) <= 65536),
    -- Evidence under a codec nothing named is an unreadable blob, which is worse
    -- than no evidence: it reads as evidence that exists.
    CHECK ((evidence IS NULL) = (evidence_codec = '')),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_alerts_tenant_id_organization_id
    ON alerts(tenant_id, organization_id);
-- The hourly ceiling's own read: how much one customer has raised recently.
-- Bounded by the ceiling itself, since nothing is stored past it.
CREATE INDEX IF NOT EXISTS idx_alerts_organization_id_received_at
    ON alerts(organization_id, received_at DESC);
-- What is in a room, which is also how the counts are recomputed after an
-- erasure takes some of it away.
CREATE INDEX IF NOT EXISTS idx_alerts_incident_id ON alerts(incident_id);
-- One machine's alert history, newest first.
CREATE INDEX IF NOT EXISTS idx_alerts_device_id_observed_at
    ON alerts(device_id, observed_at DESC);

ALTER TABLE alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE alerts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_alerts ON alerts
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));

-- What happened to the room -------------------------------------------------
-- Append-only. The incident's own columns say where it stands now; this says how
-- it got there, which is what a handover between two technicians reads.
CREATE TABLE IF NOT EXISTS incident_events (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    incident_id     UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    kind            TEXT NOT NULL CHECK (kind IN (
                        'alert_folded', 'status_change', 'assignment',
                        'comment', 'device_offline', 'resolution')),
    -- Absent when the system did it rather than a person.
    actor_id        UUID REFERENCES users(id) ON DELETE SET NULL,
    body            JSONB NOT NULL DEFAULT '{}'::jsonb
                    CHECK (jsonb_typeof(body) = 'object'),
    FOREIGN KEY (tenant_id, organization_id)
        REFERENCES organizations(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_incident_events_tenant_id_organization_id
    ON incident_events(tenant_id, organization_id);
-- The room's timeline, oldest first.
CREATE INDEX IF NOT EXISTS idx_incident_events_incident_id_at
    ON incident_events(incident_id, at);

ALTER TABLE incident_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_incident_events ON incident_events
    USING (app_tenant_visible(tenant_id))
    WITH CHECK (app_tenant_visible(tenant_id));
