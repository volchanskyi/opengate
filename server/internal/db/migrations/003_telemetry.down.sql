DROP POLICY IF EXISTS tenant_isolation_device_processes ON device_processes;
ALTER TABLE device_processes DISABLE ROW LEVEL SECURITY;
DROP TABLE IF EXISTS device_processes;
