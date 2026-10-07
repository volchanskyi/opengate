-- Keyset pages of the triage queue, read before (last_seen, id), per customer or per tenant.
CREATE INDEX IF NOT EXISTS idx_incidents_organization_id_last_seen_id
    ON incidents(organization_id, last_seen DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_incidents_tenant_id_last_seen_id
    ON incidents(tenant_id, last_seen DESC, id DESC);

-- Finds the incidents one machine is in through its alerts; it supersedes the incident-only
-- index because the same column leads both.
CREATE INDEX IF NOT EXISTS idx_alerts_incident_id_device_id
    ON alerts(incident_id, device_id);
DROP INDEX IF EXISTS idx_alerts_incident_id;
