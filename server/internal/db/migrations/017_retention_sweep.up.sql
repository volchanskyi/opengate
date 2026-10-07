-- The retention sweep spans every tenant, so these indexes lead with the timestamp.
-- Receipt time, since a retroactive finding can arrive months after it happened.
CREATE INDEX IF NOT EXISTS idx_alerts_received_at ON alerts(received_at);

-- Partial because only resolved incidents are retention candidates.
CREATE INDEX IF NOT EXISTS idx_incidents_resolved_at
    ON incidents(resolved_at) WHERE status = 'resolved';
