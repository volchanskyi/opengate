#!/usr/bin/env bash
# Create the non-superuser runtime role used by the server. The official
# Postgres image creates POSTGRES_USER as a superuser; keep that role for
# maintenance/backups and run application traffic through opengate_app.

set -euo pipefail

: "${POSTGRES_USER:?POSTGRES_USER is required}"
: "${POSTGRES_DB:?POSTGRES_DB is required}"
: "${POSTGRES_APP_PASSWORD:?POSTGRES_APP_PASSWORD is required}"

psql \
  -v ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=app_password="$POSTGRES_APP_PASSWORD" \
  --set=db_name="$POSTGRES_DB" <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'opengate_app') THEN
    CREATE ROLE opengate_app LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION;
  END IF;
END
$$;

ALTER ROLE opengate_app WITH LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'app_password';
GRANT CONNECT ON DATABASE :"db_name" TO opengate_app;
GRANT USAGE, CREATE ON SCHEMA public TO opengate_app;
SQL
