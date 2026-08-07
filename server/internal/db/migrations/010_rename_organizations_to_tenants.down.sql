-- Reverse of the tenancy rename: names only.
--
-- The columns move first. Renaming a column carries every policy built on
-- it, so the policy statements below only have the scope setting left to
-- change and the expression they are given already resolves.

ALTER TABLE device_processes RENAME CONSTRAINT device_processes_tenant_id_device_id_ts_rank_key
    TO device_processes_org_id_device_id_ts_rank_key;
ALTER TABLE device_inventory RENAME CONSTRAINT device_inventory_tenant_id_device_id_kind_name_port_proto_key
    TO device_inventory_org_id_device_id_kind_name_port_proto_key;

ALTER TABLE users                  RENAME CONSTRAINT users_tenant_id_fkey                  TO users_org_id_fkey;
ALTER TABLE groups_                RENAME CONSTRAINT groups_tenant_id_fkey                 TO groups_org_id_fkey;
ALTER TABLE devices                RENAME CONSTRAINT devices_tenant_id_fkey                TO devices_org_id_fkey;
ALTER TABLE agent_sessions         RENAME CONSTRAINT agent_sessions_tenant_id_fkey         TO agent_sessions_org_id_fkey;
ALTER TABLE web_push_subscriptions RENAME CONSTRAINT web_push_subscriptions_tenant_id_fkey TO web_push_subscriptions_org_id_fkey;
ALTER TABLE audit_events           RENAME CONSTRAINT audit_events_tenant_id_fkey           TO audit_events_org_id_fkey;
ALTER TABLE amt_devices            RENAME CONSTRAINT amt_devices_tenant_id_fkey            TO amt_devices_org_id_fkey;
ALTER TABLE enrollment_tokens      RENAME CONSTRAINT enrollment_tokens_tenant_id_fkey      TO enrollment_tokens_org_id_fkey;
ALTER TABLE security_groups        RENAME CONSTRAINT security_groups_tenant_id_fkey        TO security_groups_org_id_fkey;
ALTER TABLE security_group_members RENAME CONSTRAINT security_group_members_tenant_id_fkey TO security_group_members_org_id_fkey;
ALTER TABLE device_updates         RENAME CONSTRAINT device_updates_tenant_id_fkey         TO device_updates_org_id_fkey;
ALTER TABLE device_hardware        RENAME CONSTRAINT device_hardware_tenant_id_fkey        TO device_hardware_org_id_fkey;
ALTER TABLE device_processes       RENAME CONSTRAINT device_processes_tenant_id_fkey       TO device_processes_org_id_fkey;
ALTER TABLE device_inventory       RENAME CONSTRAINT device_inventory_tenant_id_fkey       TO device_inventory_org_id_fkey;

ALTER TABLE users                  RENAME COLUMN tenant_id TO org_id;
ALTER TABLE groups_                RENAME COLUMN tenant_id TO org_id;
ALTER TABLE devices                RENAME COLUMN tenant_id TO org_id;
ALTER TABLE agent_sessions         RENAME COLUMN tenant_id TO org_id;
ALTER TABLE web_push_subscriptions RENAME COLUMN tenant_id TO org_id;
ALTER TABLE audit_events           RENAME COLUMN tenant_id TO org_id;
ALTER TABLE amt_devices            RENAME COLUMN tenant_id TO org_id;
ALTER TABLE enrollment_tokens      RENAME COLUMN tenant_id TO org_id;
ALTER TABLE security_groups        RENAME COLUMN tenant_id TO org_id;
ALTER TABLE security_group_members RENAME COLUMN tenant_id TO org_id;
ALTER TABLE device_updates         RENAME COLUMN tenant_id TO org_id;
ALTER TABLE device_hardware        RENAME COLUMN tenant_id TO org_id;
ALTER TABLE device_processes       RENAME COLUMN tenant_id TO org_id;
ALTER TABLE device_inventory       RENAME COLUMN tenant_id TO org_id;
ALTER TABLE deleted_ids            RENAME COLUMN tenant_id TO org_id;
ALTER TABLE purge_jobs             RENAME COLUMN tenant_id TO org_id;

ALTER TABLE deleted_ids DROP CONSTRAINT deleted_ids_scope_check;
UPDATE deleted_ids SET scope = 'org' WHERE scope = 'tenant';
ALTER TABLE deleted_ids ADD CONSTRAINT deleted_ids_scope_check CHECK (scope IN ('device', 'org'));

ALTER TABLE purge_jobs DROP CONSTRAINT purge_jobs_scope_check;
UPDATE purge_jobs SET scope = 'org' WHERE scope = 'tenant';
ALTER TABLE purge_jobs ADD CONSTRAINT purge_jobs_scope_check CHECK (scope IN ('device', 'org'));

DO $$
DECLARE
    predicate CONSTANT text :=
        'org_id = current_setting(''app.current_org'')::uuid'
        ' OR current_setting(''app.is_admin'', true)::boolean';
    target RECORD;
BEGIN
    FOR target IN
        SELECT policyname, tablename
          FROM pg_policies
         WHERE schemaname = current_schema()
           AND policyname LIKE 'tenant_isolation_%'
    LOOP
        EXECUTE format('ALTER POLICY %I ON %I USING (%s) WITH CHECK (%s)',
                       target.policyname, target.tablename, predicate, predicate);
    END LOOP;
END
$$;

ALTER INDEX IF EXISTS idx_users_tenant_id_email                  RENAME TO idx_users_org_id_email;
ALTER INDEX IF EXISTS idx_devices_tenant_id_id                   RENAME TO idx_devices_org_id_id;
ALTER INDEX IF EXISTS idx_devices_tenant_id_group_id             RENAME TO idx_devices_org_id_group_id;
ALTER INDEX IF EXISTS idx_devices_tenant_id_status               RENAME TO idx_devices_org_id_status;
ALTER INDEX IF EXISTS idx_agent_sessions_tenant_id_device_id     RENAME TO idx_agent_sessions_org_id_device_id;
ALTER INDEX IF EXISTS idx_web_push_subscriptions_tenant_id_user_id RENAME TO idx_web_push_subscriptions_org_id_user_id;
ALTER INDEX IF EXISTS idx_audit_events_tenant_id_created_at      RENAME TO idx_audit_events_org_id_created_at;
ALTER INDEX IF EXISTS idx_audit_events_tenant_id_user_id         RENAME TO idx_audit_events_org_id_user_id;
ALTER INDEX IF EXISTS idx_amt_devices_tenant_id_status           RENAME TO idx_amt_devices_org_id_status;
ALTER INDEX IF EXISTS idx_enrollment_tokens_tenant_id_token      RENAME TO idx_enrollment_tokens_org_id_token;
ALTER INDEX IF EXISTS idx_enrollment_tokens_tenant_id_created_by RENAME TO idx_enrollment_tokens_org_id_created_by;
ALTER INDEX IF EXISTS idx_security_groups_tenant_id_name         RENAME TO idx_security_groups_org_id_name;
ALTER INDEX IF EXISTS idx_sgm_tenant_id_user_id                  RENAME TO idx_sgm_org_id_user_id;
ALTER INDEX IF EXISTS idx_device_updates_tenant_id_device        RENAME TO idx_device_updates_org_id_device;
ALTER INDEX IF EXISTS idx_device_updates_tenant_id_version       RENAME TO idx_device_updates_org_id_version;
ALTER INDEX IF EXISTS idx_device_hardware_tenant_id_device_id    RENAME TO idx_device_hardware_org_id_device_id;
ALTER INDEX IF EXISTS idx_device_processes_tenant_id_device_ts   RENAME TO idx_device_processes_org_id_device_ts;
ALTER INDEX IF EXISTS idx_device_inventory_tenant_device_kind    RENAME TO idx_device_inventory_org_device_kind;
ALTER INDEX IF EXISTS idx_purge_jobs_tenant                      RENAME TO idx_purge_jobs_org;
ALTER INDEX IF EXISTS uq_deleted_ids_tenant                      RENAME TO uq_deleted_ids_org;

UPDATE tenants SET name = 'Default Organization' WHERE name = 'Default Tenant';
ALTER INDEX IF EXISTS tenants_pkey RENAME TO organizations_pkey;
ALTER INDEX IF EXISTS tenants_name_key RENAME TO organizations_name_key;
ALTER TABLE tenants RENAME TO organizations;
