#!/usr/bin/env bash
# Emit the psql input that reconciles the opengate_app runtime role.
#
# The app-role password is delivered as a `\set` meta-command on stdin rather
# than as a `psql --set=app_password=…` flag: a command line is readable by any
# process sharing the Postgres pod's PID namespace, and `kubectl exec` carries
# the command verbatim in the API server's audit record. stdin crosses neither
# boundary.
#
# Usage:
#   POSTGRES_APP_PASSWORD=… deploy/scripts/pg-app-role-sql.sh \
#     | kubectl -n NS exec -i statefulset/REL-postgres -- \
#         psql -U opengate -d opengate -v ON_ERROR_STOP=1

set -euo pipefail

: "${POSTGRES_APP_PASSWORD:?POSTGRES_APP_PASSWORD is required}"

# psql's meta-command lexer processes backslash escapes inside a single-quoted
# argument, so a backslash must be doubled before a quote is escaped — the
# reverse order would re-escape the backslashes this step introduces.
escaped="${POSTGRES_APP_PASSWORD//\\/\\\\}"
escaped="${escaped//\'/\\\'}"

printf "\\\\set app_password '%s'\n" "$escaped"

cat <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'opengate_app') THEN
    CREATE ROLE opengate_app LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION;
  END IF;
END
$$;

ALTER ROLE opengate_app WITH LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'app_password';
GRANT CONNECT ON DATABASE opengate TO opengate_app;
GRANT USAGE, CREATE ON SCHEMA public TO opengate_app;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO opengate_app;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO opengate_app;

DO $$
DECLARE
  obj record;
BEGIN
  FOR obj IN
    SELECT quote_ident(schemaname) || '.' || quote_ident(tablename) AS name
    FROM pg_tables
    WHERE schemaname = 'public'
  LOOP
    EXECUTE format('ALTER TABLE %s OWNER TO opengate_app', obj.name);
  END LOOP;

  FOR obj IN
    SELECT quote_ident(sequence_schema) || '.' || quote_ident(sequence_name) AS name
    FROM information_schema.sequences
    WHERE sequence_schema = 'public'
  LOOP
    EXECUTE format('ALTER SEQUENCE %s OWNER TO opengate_app', obj.name);
  END LOOP;
END
$$;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM pg_roles
    WHERE rolname = 'opengate_app'
      AND (rolsuper OR rolbypassrls)
  ) THEN
    RAISE EXCEPTION 'database role opengate_app must be NOSUPERUSER and NOBYPASSRLS';
  END IF;
END
$$;
SQL
