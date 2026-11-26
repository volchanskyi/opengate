-- Restores the stage clock with the defaults and bounds 013 and 016 gave it.
ALTER TABLE rule_rollout
    ADD COLUMN IF NOT EXISTS stage_entered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS canary_hold_secs INTEGER NOT NULL DEFAULT 3600
        CHECK (canary_hold_secs BETWEEN 60 AND 2592000),
    ADD COLUMN IF NOT EXISTS staged_hold_secs INTEGER NOT NULL DEFAULT 21600
        CHECK (staged_hold_secs BETWEEN 60 AND 2592000);
