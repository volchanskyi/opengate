-- No agent sends a process report and nothing reads the rows, so the table goes; an alert's
-- evidence carries its own process rows.
DROP POLICY IF EXISTS tenant_isolation_device_processes ON device_processes;
DROP TABLE IF EXISTS device_processes;
