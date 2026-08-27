#!/usr/bin/env bash
# Emit the psql input that seeds the load-test administrator.
#
# The statements themselves live in the chart, at
# deploy/helm/opengate/files/loadtest-account.sql, and the chart's post-upgrade
# hook reads the same file. One copy: a column the schema added broke a chart
# hook from a distance once already, and a second copy of those statements would
# have been broken with nothing in its own diff to say so.
#
# The address and the password are delivered as `\set` meta-commands on stdin
# rather than as `psql --set=…` flags: a command line is readable by any process
# sharing the Postgres pod's PID namespace, and `kubectl exec` carries the
# command verbatim in the API server's audit record. stdin crosses neither
# boundary.
#
# Usage:
#   ACCOUNT_EMAIL=… ACCOUNT_PASSWORD=… deploy/scripts/loadtest-account-sql.sh \
#     | kubectl -n NS exec -i statefulset/REL-postgres -- \
#         psql -U opengate -d opengate -v ON_ERROR_STOP=1

set -euo pipefail

: "${ACCOUNT_EMAIL:?ACCOUNT_EMAIL is required}"
: "${ACCOUNT_PASSWORD:?ACCOUNT_PASSWORD is required}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SQL_FILE="$SCRIPT_DIR/../helm/opengate/files/loadtest-account.sql"

if [ ! -f "$SQL_FILE" ]; then
  echo "loadtest-account-sql: statements not found at $SQL_FILE" >&2
  exit 1
fi

# psql's meta-command lexer processes backslash escapes inside a single-quoted
# argument, so a backslash must be doubled before a quote is escaped — the
# reverse order would re-escape the backslashes this step introduces.
psql_quote() {
  local escaped="${1//\\/\\\\}"
  printf "'%s'" "${escaped//\'/\\\'}"
}

printf '\\set email %s\n' "$(psql_quote "$ACCOUNT_EMAIL")"
printf '\\set account_password %s\n' "$(psql_quote "$ACCOUNT_PASSWORD")"

cat "$SQL_FILE"
