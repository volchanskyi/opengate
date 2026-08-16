CREATE INDEX IF NOT EXISTS idx_alerts_incident_id ON alerts(incident_id);
DROP INDEX IF EXISTS idx_alerts_incident_id_device_id;
DROP INDEX IF EXISTS idx_incidents_tenant_id_last_seen_id;
DROP INDEX IF EXISTS idx_incidents_organization_id_last_seen_id;
