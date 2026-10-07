-- Idempotent first-start bootstrap mirroring deploy/postgres/init.sql, so the chart renders
-- self-contained; POSTGRES_USER and POSTGRES_DB already create the role and database.

CREATE SCHEMA IF NOT EXISTS public;

-- Restrict default privileges: new tables readable only by the owner.
ALTER DEFAULT PRIVILEGES REVOKE ALL ON TABLES FROM PUBLIC;
