DROP POLICY IF EXISTS tenant_isolation_device_inventory ON device_inventory;
ALTER TABLE IF EXISTS device_inventory DISABLE ROW LEVEL SECURITY;
DROP TABLE IF EXISTS device_inventory;
