-- Multi-tenancy foundation: organizations + org_id + forced RLS.

CREATE TABLE IF NOT EXISTS organizations (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO organizations (id, name)
VALUES ('00000000-0000-0000-0000-000000000002', 'Default Organization')
ON CONFLICT (id) DO NOTHING;

ALTER TABLE users ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE users SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE users ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_users_org_id_email ON users(org_id, email);

ALTER TABLE groups_ ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE groups_ g SET org_id = u.org_id FROM users u WHERE g.owner_id = u.id AND g.org_id IS NULL;
UPDATE groups_ SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE groups_ ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE groups_ ADD CONSTRAINT groups_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_groups_org_id_owner_id ON groups_(org_id, owner_id);

ALTER TABLE devices ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE devices d SET org_id = g.org_id FROM groups_ g WHERE d.group_id = g.id AND d.org_id IS NULL;
UPDATE devices SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE devices ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE devices ADD CONSTRAINT devices_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_devices_org_id_id ON devices(org_id, id);
CREATE INDEX IF NOT EXISTS idx_devices_org_id_group_id ON devices(org_id, group_id);
CREATE INDEX IF NOT EXISTS idx_devices_org_id_status ON devices(org_id, status);

ALTER TABLE agent_sessions ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE agent_sessions s SET org_id = d.org_id FROM devices d WHERE s.device_id = d.id AND s.org_id IS NULL;
UPDATE agent_sessions SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE agent_sessions ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE agent_sessions ADD CONSTRAINT agent_sessions_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_org_id_device_id ON agent_sessions(org_id, device_id);

ALTER TABLE web_push_subscriptions ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE web_push_subscriptions s SET org_id = u.org_id FROM users u WHERE s.user_id = u.id AND s.org_id IS NULL;
UPDATE web_push_subscriptions SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE web_push_subscriptions ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE web_push_subscriptions ADD CONSTRAINT web_push_subscriptions_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_web_push_subscriptions_org_id_user_id ON web_push_subscriptions(org_id, user_id);

ALTER TABLE audit_events ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE audit_events e SET org_id = u.org_id FROM users u WHERE e.user_id = u.id AND e.org_id IS NULL;
UPDATE audit_events SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE audit_events ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE audit_events ADD CONSTRAINT audit_events_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_audit_events_org_id_created_at ON audit_events(org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_org_id_user_id ON audit_events(org_id, user_id);

ALTER TABLE amt_devices ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE amt_devices SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE amt_devices ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE amt_devices ADD CONSTRAINT amt_devices_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_amt_devices_org_id_status ON amt_devices(org_id, status);

ALTER TABLE enrollment_tokens ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE enrollment_tokens t SET org_id = u.org_id FROM users u WHERE t.created_by = u.id AND t.org_id IS NULL;
UPDATE enrollment_tokens SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE enrollment_tokens ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE enrollment_tokens ADD CONSTRAINT enrollment_tokens_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_org_id_token ON enrollment_tokens(org_id, token);
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_org_id_created_by ON enrollment_tokens(org_id, created_by);

ALTER TABLE security_groups ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE security_groups SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE security_groups ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE security_groups ADD CONSTRAINT security_groups_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_security_groups_org_id_name ON security_groups(org_id, name);

ALTER TABLE security_group_members ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE security_group_members m SET org_id = g.org_id FROM security_groups g WHERE m.group_id = g.id AND m.org_id IS NULL;
UPDATE security_group_members SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE security_group_members ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE security_group_members ADD CONSTRAINT security_group_members_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_sgm_org_id_user_id ON security_group_members(org_id, user_id);

ALTER TABLE device_updates ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE device_updates u SET org_id = d.org_id FROM devices d WHERE u.device_id = d.id AND u.org_id IS NULL;
UPDATE device_updates SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE device_updates ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE device_updates ADD CONSTRAINT device_updates_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_device_updates_org_id_device ON device_updates(org_id, device_id);
CREATE INDEX IF NOT EXISTS idx_device_updates_org_id_version ON device_updates(org_id, version);

ALTER TABLE device_hardware ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE device_hardware h SET org_id = d.org_id FROM devices d WHERE h.device_id = d.id AND h.org_id IS NULL;
UPDATE device_hardware SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE device_hardware ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE device_hardware ADD CONSTRAINT device_hardware_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_device_hardware_org_id_device_id ON device_hardware(org_id, device_id);

ALTER TABLE device_logs ADD COLUMN IF NOT EXISTS org_id UUID;
UPDATE device_logs l SET org_id = d.org_id FROM devices d WHERE l.device_id = d.id AND l.org_id IS NULL;
UPDATE device_logs SET org_id = '00000000-0000-0000-0000-000000000002' WHERE org_id IS NULL;
ALTER TABLE device_logs ALTER COLUMN org_id SET NOT NULL;
ALTER TABLE device_logs ADD CONSTRAINT device_logs_org_id_fkey FOREIGN KEY (org_id) REFERENCES organizations(id);
CREATE INDEX IF NOT EXISTS idx_device_logs_org_id_device_id ON device_logs(org_id, device_id);
CREATE INDEX IF NOT EXISTS idx_device_logs_org_id_level ON device_logs(org_id, device_id, level);
CREATE INDEX IF NOT EXISTS idx_device_logs_org_id_timestamp ON device_logs(org_id, device_id, timestamp);

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_users ON users
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE groups_ ENABLE ROW LEVEL SECURITY;
ALTER TABLE groups_ FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_groups ON groups_
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE devices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_devices ON devices
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE agent_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_agent_sessions ON agent_sessions
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE web_push_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE web_push_subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_web_push_subscriptions ON web_push_subscriptions
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_audit_events ON audit_events
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE amt_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE amt_devices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_amt_devices ON amt_devices
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE enrollment_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE enrollment_tokens FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_enrollment_tokens ON enrollment_tokens
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE security_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE security_groups FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_security_groups ON security_groups
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE security_group_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE security_group_members FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_security_group_members ON security_group_members
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE device_updates ENABLE ROW LEVEL SECURITY;
ALTER TABLE device_updates FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_device_updates ON device_updates
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE device_hardware ENABLE ROW LEVEL SECURITY;
ALTER TABLE device_hardware FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_device_hardware ON device_hardware
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);

ALTER TABLE device_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE device_logs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_device_logs ON device_logs
    USING (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean)
    WITH CHECK (org_id = current_setting('app.current_org')::uuid OR current_setting('app.is_admin', true)::boolean);
