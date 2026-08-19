DROP INDEX IF EXISTS idx_alerts_organization_id_received_at_rule_id;

DROP TABLE IF EXISTS rule_binding_clamps;

ALTER TABLE rule_rollout
    DROP COLUMN IF EXISTS staged_hold_secs,
    DROP COLUMN IF EXISTS canary_hold_secs,
    DROP COLUMN IF EXISTS staged_percent,
    DROP COLUMN IF EXISTS canary_percent;

DROP TABLE IF EXISTS organization_alert_limits;
DROP TABLE IF EXISTS device_tags;
DROP TABLE IF EXISTS device_tag_labels;
