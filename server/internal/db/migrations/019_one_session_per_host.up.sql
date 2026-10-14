-- A host takes one session; where an earlier release left several, the newest stays.
DELETE FROM agent_sessions s
 USING agent_sessions newer
 WHERE newer.device_id = s.device_id
   AND (newer.created_at, newer.token) > (s.created_at, s.token);

DROP INDEX IF EXISTS idx_agent_sessions_device_id;
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_sessions_one_per_device ON agent_sessions(device_id);

-- One bit per direct-link outcome this session has reported, so each is counted once.
ALTER TABLE agent_sessions ADD COLUMN IF NOT EXISTS link_outcomes SMALLINT NOT NULL DEFAULT 0;
