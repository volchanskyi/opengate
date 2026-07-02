#!/usr/bin/env bash
# Tests for the Postgres runtime role RLS contract.
#
# Bug history: tenant-scoped repository tests use explicit org predicates, so
# they do not prove production RLS is active if the app role remains a superuser.
# The deployment must demote/verify the runtime role as NOSUPERUSER/NOBYPASSRLS.
#
# Run: ./scripts/tests/postgres-runtime-role.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
HELM_APP_ROLE="$REPO_ROOT/deploy/helm/opengate/files/zz-app-role.sh"
POSTGRES_STATEFULSET="$REPO_ROOT/deploy/helm/opengate/templates/postgres-statefulset.yaml"
SERVER_DEPLOYMENT="$REPO_ROOT/deploy/helm/opengate/templates/server-deployment.yaml"
CD_WORKFLOW="$REPO_ROOT/.github/workflows/cd.yml"

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

assert_file_contains() {
  local name="$1"
  local file="$2"
  local expected="$3"
  if [ -f "$file" ] && grep -qF -- "$expected" "$file"; then
    pass "$name"
  else
    fail "$name"
  fi
}

echo "postgres-runtime-role:"

role_flags="NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION"

assert_file_contains \
  "Helm bootstrap creates a non-superuser app role" \
  "$HELM_APP_ROLE" \
  "CREATE ROLE opengate_app LOGIN $role_flags"
assert_file_contains \
  "Postgres pod exposes app-role password to bootstrap" \
  "$POSTGRES_STATEFULSET" \
  "POSTGRES_APP_PASSWORD"
assert_file_contains \
  "Postgres init shell runs as standalone script" \
  "$POSTGRES_STATEFULSET" \
  "defaultMode: 0555"
assert_file_contains \
  "Server connects as the app role" \
  "$SERVER_DEPLOYMENT" \
  "postgres://opengate_app:\$(POSTGRES_APP_PASSWORD)@"

guard_count="$(grep -c "Ensure database role honors RLS" "$CD_WORKFLOW" || true)"
if [ "$guard_count" -eq 2 ]; then
  pass "CD guards both staging and production database roles"
else
  fail "CD guards both staging and production database roles"
fi

assert_file_contains \
  "CD guard verifies app role superuser and BYPASSRLS are off" \
  "$CD_WORKFLOW" \
  "rolsuper OR rolbypassrls"
assert_file_contains \
  "CD guard can demote existing over-privileged app role" \
  "$CD_WORKFLOW" \
  "ALTER ROLE opengate_app WITH LOGIN $role_flags"
assert_file_contains \
  "CD guard lets the app role own migrated tables" \
  "$CD_WORKFLOW" \
  "ALTER TABLE %s OWNER TO opengate_app"
assert_file_contains \
  "CD sync writes app-role password into the Kubernetes Secret" \
  "$CD_WORKFLOW" \
  "--from-literal=POSTGRES_APP_PASSWORD="

if grep -R -qF "ALTER ROLE opengate NOSUPERUSER" \
  "$REPO_ROOT/deploy/helm/opengate/files" \
  "$CD_WORKFLOW"; then
  fail "deployment keeps opengate as the maintenance/backup role"
else
  pass "deployment keeps opengate as the maintenance/backup role"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
