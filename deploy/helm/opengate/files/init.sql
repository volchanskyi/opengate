-- Sourced from deploy/postgres/init.sql — kept in sync so the chart is
-- self-contained for `helm template` validation. Mounted into the Postgres
-- StatefulSet via a ConfigMap at /docker-entrypoint-initdb.d/init.sql.
--
-- Idempotent bootstrap for the opengate database. Runs once on first start.
-- POSTGRES_USER / POSTGRES_DB already create the role and database; this
-- script only sets recommended defaults.

CREATE SCHEMA IF NOT EXISTS public;

-- Restrict default privileges: new tables readable only by the owner.
ALTER DEFAULT PRIVILEGES REVOKE ALL ON TABLES FROM PUBLIC;
