-- Tables drop newest-dependency-first; app_tenant_visible stays because the rules policies use it.
DROP TABLE IF EXISTS incident_events;
DROP TABLE IF EXISTS alerts;
DROP TABLE IF EXISTS incidents;
