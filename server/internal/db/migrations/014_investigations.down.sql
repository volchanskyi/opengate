-- Dropped newest-dependency-first: incident events point at incidents, alerts
-- point at both incidents and devices.
--
-- app_tenant_visible stays. Migration 013 creates it and its own tables' policies
-- are built on it, so dropping it here would leave those policies pointing at a
-- function that no longer exists.
DROP TABLE IF EXISTS incident_events;
DROP TABLE IF EXISTS alerts;
DROP TABLE IF EXISTS incidents;
