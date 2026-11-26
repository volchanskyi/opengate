-- An administrator moves a rule's stage, so the rollout keeps its shares and nothing times a stage.
ALTER TABLE rule_rollout
    DROP COLUMN IF EXISTS staged_hold_secs,
    DROP COLUMN IF EXISTS canary_hold_secs,
    DROP COLUMN IF EXISTS stage_entered_at;
