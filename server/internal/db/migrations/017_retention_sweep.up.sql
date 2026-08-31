-- Age-based reclamation of the investigation tables.
--
-- Erasure already cascades off a machine or a customer. This is the other axis:
-- rows nobody deleted, kept only as long as they are declared to be kept. The
-- sweep that reads these runs across every tenant at once, so both indexes lead
-- with the timestamp rather than with a tenant or a customer — an index whose
-- leading column the sweep cannot constrain is one the sweep cannot use.

-- Receipt, not event time: a retroactive finding can arrive legitimately months
-- after it happened, and the horizon is about how long a row has been held.
CREATE INDEX IF NOT EXISTS idx_alerts_received_at ON alerts(received_at);

-- Partial, because only a closed room is ever a candidate: an open room is
-- somebody's outstanding work and age alone never removes it. That keeps this
-- index proportional to what is closed rather than to what exists.
CREATE INDEX IF NOT EXISTS idx_incidents_resolved_at
    ON incidents(resolved_at) WHERE status = 'resolved';
