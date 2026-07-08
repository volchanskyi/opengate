-- Retire the central device-log cache. Raw logs are brokered on demand and
-- streamed straight through to the caller, so nothing backs them centrally;
-- isolation is the connection scope, not an RLS row.
DROP TABLE IF EXISTS device_logs;
