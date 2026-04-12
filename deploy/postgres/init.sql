-- Idempotent bootstrap for the opengate database.
-- Runs once via docker-entrypoint-initdb.d on first container start.
-- The POSTGRES_USER / POSTGRES_DB env vars already create the role and
-- database; this script only sets recommended defaults.

-- Ensure the public schema exists (docker image creates it, but be safe).
CREATE SCHEMA IF NOT EXISTS public;

-- Restrict default privileges: new tables readable only by the owner.
ALTER DEFAULT PRIVILEGES REVOKE ALL ON TABLES FROM PUBLIC;
