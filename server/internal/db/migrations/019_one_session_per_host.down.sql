ALTER TABLE agent_sessions DROP COLUMN IF EXISTS link_outcomes;
DROP INDEX IF EXISTS idx_agent_sessions_one_per_device;
CREATE INDEX IF NOT EXISTS idx_agent_sessions_device_id ON agent_sessions(device_id);
