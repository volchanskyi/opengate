DROP POLICY IF EXISTS tenant_isolation_device_logs ON device_logs;
ALTER TABLE device_logs DISABLE ROW LEVEL SECURITY;
ALTER TABLE device_logs DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_device_hardware ON device_hardware;
ALTER TABLE device_hardware DISABLE ROW LEVEL SECURITY;
ALTER TABLE device_hardware DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_device_updates ON device_updates;
ALTER TABLE device_updates DISABLE ROW LEVEL SECURITY;
ALTER TABLE device_updates DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_security_group_members ON security_group_members;
ALTER TABLE security_group_members DISABLE ROW LEVEL SECURITY;
ALTER TABLE security_group_members DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_security_groups ON security_groups;
ALTER TABLE security_groups DISABLE ROW LEVEL SECURITY;
ALTER TABLE security_groups DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_enrollment_tokens ON enrollment_tokens;
ALTER TABLE enrollment_tokens DISABLE ROW LEVEL SECURITY;
ALTER TABLE enrollment_tokens DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_amt_devices ON amt_devices;
ALTER TABLE amt_devices DISABLE ROW LEVEL SECURITY;
ALTER TABLE amt_devices DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_audit_events ON audit_events;
ALTER TABLE audit_events DISABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_web_push_subscriptions ON web_push_subscriptions;
ALTER TABLE web_push_subscriptions DISABLE ROW LEVEL SECURITY;
ALTER TABLE web_push_subscriptions DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_agent_sessions ON agent_sessions;
ALTER TABLE agent_sessions DISABLE ROW LEVEL SECURITY;
ALTER TABLE agent_sessions DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_devices ON devices;
ALTER TABLE devices DISABLE ROW LEVEL SECURITY;
ALTER TABLE devices DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_groups ON groups_;
ALTER TABLE groups_ DISABLE ROW LEVEL SECURITY;
ALTER TABLE groups_ DROP COLUMN IF EXISTS org_id;

DROP POLICY IF EXISTS tenant_isolation_users ON users;
ALTER TABLE users DISABLE ROW LEVEL SECURITY;
ALTER TABLE users DROP COLUMN IF EXISTS org_id;

DROP TABLE IF EXISTS organizations;
