#!/usr/bin/env bash
# Tests for deploy/scripts/loadtest-account-sql.sh.
#
# The load-test administrator is seeded twice: by the chart's post-upgrade hook,
# and by the staging deploy once the browser suite has finished — because the
# suite's database reset destroys it, and the nightly load run has nobody to
# mint an enrolment token against without it.
#
# Two copies of that SQL drift. A column the schema added broke one copy from a
# distance once already, and a second copy would have been broken with nothing
# to say so. So both callers read one file, and this holds them to it.
#
# The password reaches psql over stdin as a \set meta-command for the same
# reason the app-role password does: a command line is readable by every process
# in the Postgres pod and is recorded verbatim in the API server's audit entry
# for the exec subresource.
#
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

# Both credentials are required. A silent empty password writes an account
# nobody can sign in as, and the load run fails a day later on a 401.
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

# psql's meta-command lexer reads backslash escapes inside a single-quoted
# argument, so a quote in the generated password has to arrive escaped and a
# backslash has to arrive doubled — in that order, or the escaping is escaped.
TRICKY="$(ACCOUNT_PASSWORD="a'b\\c" ACCOUNT_EMAIL='svc@service.invalid' "$EMITTER")"
assert_contains "a quote and a backslash in the password survive intact" \
  "$TRICKY" "\\set account_password 'a\\'b\\\\c'"

# The statements themselves are the file's, byte for byte. Anything else means
# a second copy has appeared.
EMITTED_SQL="$(grep -v '^\\set ' <<<"$OUTPUT")"
if [ "$EMITTED_SQL" = "$(cat "$SQL_FILE")" ]; then
  pass "the emitter sends the shared file and nothing else"
else
  fail "the emitter's SQL differs from the shared file — there are two copies again"
fi

# ...and the chart hook reads the same file rather than restating it.
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

# The statements have to be safe to run twice: the hook fires on every upgrade,
# and the deploy runs the same file again after every browser suite.
for clause in "ON CONFLICT (email) DO UPDATE" "ON CONFLICT DO NOTHING"; do
  assert_contains "seeding is repeatable ($clause)" "$(cat "$SQL_FILE")" "$clause"
done

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
