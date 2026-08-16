-- The orders the triage queue is actually read in.
--
-- An incident list is a page of the most recent activity, and a page is a
-- keyset: an offset over a queue that is being written to skips and repeats
-- rows silently, so the read is "everything before (last_seen, id)" and the
-- index has to carry both columns in that order. Without one, a page of fifty
-- costs a full scan and a sort of every open room the customer has, which is the
-- shape that turns a 200 ms budget into a scan that grows with the table.
--
-- Two of them, because the queue is asked two different questions. A technician
-- working one customer asks for that customer's rooms; a technician covering an
-- estate of customers asks for all of them at once. Neither can be answered from
-- the other's index — the customer-leading one is ordered by customer first, so
-- reading it across customers would sort — and both are the same read otherwise.
CREATE INDEX IF NOT EXISTS idx_incidents_organization_id_last_seen_id
    ON incidents(organization_id, last_seen DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_incidents_tenant_id_last_seen_id
    ON incidents(tenant_id, last_seen DESC, id DESC);

-- Which rooms one machine is in. The device page shows a machine's incidents,
-- and a room is not keyed on a machine — a customer-wide event is one room
-- across forty of them — so the question is answered through the alerts the room
-- holds. Carrying the device on the same index the room's alerts are already
-- found by makes that a lookup rather than a read of every alert in the room.
--
-- It replaces the incident-only index rather than joining it: the same column
-- leads both, so every read the narrower one served is served by this one.
CREATE INDEX IF NOT EXISTS idx_alerts_incident_id_device_id
    ON alerts(incident_id, device_id);
DROP INDEX IF EXISTS idx_alerts_incident_id;
