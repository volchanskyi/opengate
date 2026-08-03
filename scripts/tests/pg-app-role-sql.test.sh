#!/usr/bin/env bash
# Tests for deploy/scripts/pg-app-role-sql.sh.
#
# The emitter exists so the app-role password reaches psql over stdin instead of
# on the command line: a `psql --set=app_password=…` argv is visible to every
# process in the Postgres pod and is recorded verbatim in the Kubernetes API
# server's audit entry for the exec subresource.
#
# Run: ./scripts/tests/pg-app-role-sql.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EMITTER="$SCRIPT_DIR/../../deploy/scripts/pg-app-role-sql.sh"

if [ ! -x "$EMITTER" ]; then
  echo "FAIL: $EMITTER not found or not executable" >&2
  exit 1
fi

PASS=0
FAIL=0
FAILURES=()

pass() {
  PASS=$((PASS + 1))
  printf '  ok   %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  FAILURES+=("$1")
  printf '  FAIL %s\n' "$1" >&2
}

assert_contains() {
  local name="$1" haystack="$2" needle="$3"
  if grep -qF -- "$needle" <<<"$haystack"; then
    pass "$name"
  else
    fail "$name (missing: $needle)"
  fi
}

assert_not_contains() {
  local name="$1" haystack="$2" needle="$3"
  if grep -qF -- "$needle" <<<"$haystack"; then
    fail "$name (found: $needle)"
  else
    pass "$name"
  fi
}

# --- password never lands on a command line ---------------------------------

out="$(POSTGRES_APP_PASSWORD='simplepass' "$EMITTER")"
assert_contains "sets the psql variable from stdin" "$out" "\\set app_password 'simplepass'"
assert_contains "emits the ALTER ROLE using the psql variable" "$out" "PASSWORD :'app_password'"
assert_not_contains "never emits a --set command-line flag" "$out" "--set=app_password"

# --- SQL body is carried intact ---------------------------------------------

assert_contains "creates the role when absent" "$out" "CREATE ROLE opengate_app"
assert_contains "keeps the role subject to RLS" "$out" "NOSUPERUSER NOBYPASSRLS"
assert_contains "reassigns table ownership" "$out" "ALTER TABLE %s OWNER TO opengate_app"
assert_contains "reassigns sequence ownership" "$out" "ALTER SEQUENCE %s OWNER TO opengate_app"
assert_contains "fails loudly if the role can bypass RLS" "$out" \
  "database role opengate_app must be NOSUPERUSER and NOBYPASSRLS"

# --- psql meta-command quoting ----------------------------------------------

out="$(POSTGRES_APP_PASSWORD="pa'ss" "$EMITTER")"
assert_contains "escapes a single quote for the psql lexer" "$out" "\\set app_password 'pa\\'ss'"

out="$(POSTGRES_APP_PASSWORD='pa\ss' "$EMITTER")"
assert_contains "escapes a backslash for the psql lexer" "$out" "\\set app_password 'pa\\\\ss'"

# A password whose raw form would terminate the quoted argument must never
# appear unescaped — that would let the value be read as psql meta-commands.
out="$(POSTGRES_APP_PASSWORD="x'; \\echo pwned" "$EMITTER")"
assert_not_contains "never emits an unescaped quote-break" "$out" "'x'; \\echo pwned'"

# --- required input ----------------------------------------------------------

if (
  unset POSTGRES_APP_PASSWORD
  "$EMITTER" >/dev/null 2>&1
); then
  fail "exits non-zero when POSTGRES_APP_PASSWORD is unset"
else
  pass "exits non-zero when POSTGRES_APP_PASSWORD is unset"
fi

if POSTGRES_APP_PASSWORD='' "$EMITTER" >/dev/null 2>&1; then
  fail "exits non-zero when POSTGRES_APP_PASSWORD is empty"
else
  pass "exits non-zero when POSTGRES_APP_PASSWORD is empty"
fi

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
exit 0
