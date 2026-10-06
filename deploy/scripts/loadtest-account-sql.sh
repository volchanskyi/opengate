#!/usr/bin/env bash

# Emits the psql input that seeds the load-test administrator from the chart's SQL file.
# Address and password travel as `\set` commands on stdin, out of process lists and audit records.

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

# psql reads escapes inside quotes: backslashes are doubled first, then quotes are escaped.
psql_quote() {
  local escaped="${1//\\/\\\\}"
  printf "'%s'" "${escaped//\'/\\\'}"
}

printf '\\set email %s\n' "$(psql_quote "$ACCOUNT_EMAIL")"
printf '\\set account_password %s\n' "$(psql_quote "$ACCOUNT_PASSWORD")"

cat "$SQL_FILE"
