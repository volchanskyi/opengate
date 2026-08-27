#!/usr/bin/env bash
# Tests for the staging deploy job in .github/workflows/cd.yml.
#
# Bug history: WS-0 added security_groups.tenant_id NOT NULL. The CD staging reset
# truncates security_groups and then reseeds Administrators; omitting tenant_id
# makes post-migration CD fail before Playwright E2E starts.
#
# The same truncation also takes the load-test administrator the post-upgrade
# hook seeded four steps earlier. Putting it back is not this job's work: the
# nightly load run seeds that account itself, immediately before it spends it,
# so it stands on nothing a deploy did hours earlier.
#
# And the two machines the suite reads its device pages against are created for
# the run and removed after it, whatever the verdict — a machine left behind is
# a device row the next run's fleet assertions inherit.
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

# --- the run's own machines, and the administrator it borrows the table from ---

# The line a step's name is on, or empty. Steps in this workflow are at six
# spaces, so a name matched at any other depth is something else.
step_line() {
  { grep -nF -- "      - name: $1" "$WORKFLOW" || true; } | head -n 1 | cut -d: -f1
}

# Everything from a step's name up to the next step at the same depth.
step_body() {
  awk -v want="      - name: $1" '
    $0 == want { in_step = 1; next }
    in_step && /^      - / { exit }
    in_step { print }
  ' "$WORKFLOW"
}

assert_step_always() {
  local step="$1"
  if [ -z "$(step_line "$step")" ]; then
    fail "'$step' step exists"
    return
  fi
  pass "'$step' step exists"
  if grep -qE '^[[:space:]]+if:[[:space:]]+always\(\)[[:space:]]*$' <<<"$(step_body "$step")"; then
    pass "'$step' runs whatever the suite's verdict was"
  else
    fail "'$step' is skipped when the suite fails, so a red run leaves its residue behind"
  fi
}

ENROL_STEP="Enrol two machines against staging"
RESET_LINE="$(step_line "Reset staging DB for E2E")"
ENROL_LINE="$(step_line "$ENROL_STEP")"
E2E_LINE="$(step_line "Run Playwright E2E against staging")"

if [ -n "$ENROL_LINE" ]; then
  pass "'$ENROL_STEP' step exists"
else
  fail "'$ENROL_STEP' step exists"
fi

# The reset truncates devices, so the machines have to arrive after it; and the
# suite reads them, so they have to arrive before it. The bootstrap operator is
# registered in the same step, which is what makes it the first row of a table
# the reset just emptied — and therefore an administrator.
if [ -n "$RESET_LINE" ] && [ -n "$ENROL_LINE" ] && [ -n "$E2E_LINE" ] \
  && [ "$RESET_LINE" -lt "$ENROL_LINE" ] && [ "$ENROL_LINE" -lt "$E2E_LINE" ]; then
  pass "the machines arrive after the reset and before the suite"
else
  fail "the machines are not brought up between the reset and the suite"
fi

if grep -qF '/api/v1/enrollment-tokens' <<<"$(step_body "$ENROL_STEP")"; then
  pass "the machines enrol with a token minted through the public endpoint"
else
  fail "the machines enrol by some path other than the public endpoint — no key may leave the cluster"
fi

assert_step_always "Remove the staging machines"
# The account the nightly load run mints against goes down with the reset above
# and is not put back here. The run seeds its own before it spends it, so a copy
# issued from this job as well would be a second place the same statements are
# written and a second place they can drift.
if grep -qF 'loadtest-account-sql.sh' "$WORKFLOW"; then
  fail "the deploy seeds the load-test administrator, which the run that needs it does for itself"
else
  pass "the deploy leaves the load-test administrator to the run that needs it"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
