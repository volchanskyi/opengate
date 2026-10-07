#!/usr/bin/env bash
# Tests for deploy/scripts/loadtest-account-sql.sh.
# Run: ./scripts/tests/loadtest-account-sql.test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
EMITTER="$REPO_ROOT/deploy/scripts/loadtest-account-sql.sh"
SQL_FILE="$REPO_ROOT/deploy/helm/opengate/files/loadtest-account.sql"
HOOK="$REPO_ROOT/deploy/helm/opengate/templates/loadtest-service-account-job.yaml"

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
    fail "$name"
  fi
}

echo "loadtest-account-sql:"

if [ ! -x "$EMITTER" ]; then
  fail "the emitter exists and is executable"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi
pass "the emitter exists and is executable"

if [ ! -f "$SQL_FILE" ]; then
  fail "the seeding SQL has a file of its own"
  printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
  exit 1
fi
pass "the seeding SQL has a file of its own"

if env -u ACCOUNT_PASSWORD ACCOUNT_EMAIL=svc@service.invalid "$EMITTER" >/dev/null 2>&1; then
  fail "the emitter refuses to run without ACCOUNT_PASSWORD"
else
  pass "the emitter refuses to run without ACCOUNT_PASSWORD"
fi

if env -u ACCOUNT_EMAIL ACCOUNT_PASSWORD=secret "$EMITTER" >/dev/null 2>&1; then
  fail "the emitter refuses to run without ACCOUNT_EMAIL"
else
  pass "the emitter refuses to run without ACCOUNT_EMAIL"
fi

OUTPUT="$(ACCOUNT_PASSWORD='p@ss' ACCOUNT_EMAIL='svc@service.invalid' "$EMITTER")"

assert_contains "the password is delivered as a psql variable" \
  "$OUTPUT" "\\set account_password 'p@ss'"
assert_contains "the address is delivered as a psql variable" \
  "$OUTPUT" "\\set email 'svc@service.invalid'"

# psql's meta-command lexer reads backslash escapes in a quoted argument, so a quote is escaped
# and a backslash doubled, in that order.
TRICKY="$(ACCOUNT_PASSWORD="a'b\\c" ACCOUNT_EMAIL='svc@service.invalid' "$EMITTER")"
assert_contains "a quote and a backslash in the password survive intact" \
  "$TRICKY" "\\set account_password 'a\\'b\\\\c'"

EMITTED_SQL="$(grep -v '^\\set ' <<<"$OUTPUT")"
if [ "$EMITTED_SQL" = "$(cat "$SQL_FILE")" ]; then
  pass "the emitter sends the shared file and nothing else"
else
  fail "the emitter's SQL differs from the shared file — there are two copies again"
fi

if grep -qF '.Files.Get "files/loadtest-account.sql"' "$HOOK"; then
  pass "the chart hook reads the shared file"
else
  fail "the chart hook does not read the shared file, so its copy drifts"
fi

if grep -qiE 'INSERT[[:space:]]+INTO' "$HOOK"; then
  fail "the chart hook still carries its own INSERT — the copy it replaced is back"
else
  pass "the chart hook carries no INSERT of its own"
fi

for clause in "ON CONFLICT (email) DO UPDATE" "ON CONFLICT DO NOTHING"; do
  assert_contains "seeding is repeatable ($clause)" "$(cat "$SQL_FILE")" "$clause"
done

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
