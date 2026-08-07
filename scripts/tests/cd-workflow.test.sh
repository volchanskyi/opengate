#!/usr/bin/env bash
# Tests for staging reset seed SQL in .github/workflows/cd.yml.
#
# Bug history: WS-0 added security_groups.tenant_id NOT NULL. The CD staging reset
# truncates security_groups and then reseeds Administrators; omitting tenant_id
# makes post-migration CD fail before Playwright E2E starts.
#
# Run: ./scripts/tests/cd-workflow.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKFLOW="$SCRIPT_DIR/../../.github/workflows/cd.yml"

if [ ! -f "$WORKFLOW" ]; then
  echo "FAIL: $WORKFLOW not found" >&2
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

assert_reset_contains() {
  local name="$1"
  local expected="$2"
  if grep -qF "$expected" <<<"$RESET_BLOCK"; then
    pass "$name"
  else
    fail "$name"
  fi
}

extract_reset_block() {
  awk '
    /^      - name: Reset staging DB for E2E$/ { in_reset = 1 }
    in_reset { print }
    /^      - name: Run Playwright E2E against staging$/ { exit }
  ' "$WORKFLOW"
}

echo "cd-workflow:"

RESET_BLOCK="$(extract_reset_block)"
if [ -n "$RESET_BLOCK" ]; then
  pass "staging DB reset step exists"
else
  fail "staging DB reset step exists"
fi

assert_reset_contains \
  "Administrators reseed includes tenant_id" \
  "INSERT INTO security_groups (id, tenant_id, name, description, is_system)"
assert_reset_contains \
  "Administrators reseed uses seeded default tenant" \
  "VALUES ('00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000002', 'Administrators', 'Full system access', TRUE)"

if grep -qE '^[[:space:]]+tenants[[:space:],]*$' <<<"$RESET_BLOCK"; then
  fail "staging reset preserves the migration-seeded default tenant"
else
  pass "staging reset preserves the migration-seeded default tenant"
fi

# The app-role password must reach psql over stdin. A --set flag would put it in
# the Postgres pod's process list and in the API server's exec audit record.
if grep -qF -- '--set=app_password' "$WORKFLOW"; then
  fail "app-role password never rides the psql command line"
else
  pass "app-role password never rides the psql command line"
fi

if [ "$(grep -cF -- 'deploy/scripts/pg-app-role-sql.sh' "$WORKFLOW")" -eq 2 ]; then
  pass "both staging and production pipe the role SQL from the emitter"
else
  fail "both staging and production pipe the role SQL from the emitter"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
