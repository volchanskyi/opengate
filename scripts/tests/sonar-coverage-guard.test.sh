#!/usr/bin/env bash
# Tests for scripts/sonar-coverage-guard.sh. Plain bash; no network — the
# new_coverage value is injected via NEW_COVERAGE_OVERRIDE or a stubbed CURL_BIN.
# Run: ./scripts/tests/sonar-coverage-guard.test.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GUARD="$SCRIPT_DIR/../sonar-coverage-guard.sh"
[ -f "$GUARD" ] || { echo "FAIL: $GUARD not found" >&2; exit 1; }

PASS=0
FAIL=0
FAILURES=()
pass() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$1"; }
fail() { FAIL=$((FAIL + 1)); FAILURES+=("$1"); printf '  FAIL %s\n' "$1" >&2; }
assert_ok()   { local n="$1"; shift; if "$@" >/dev/null 2>&1; then pass "$n"; else fail "$n (expected 0, got $?)"; fi; }
assert_fail() { local n="$1"; shift; if "$@" >/dev/null 2>&1; then fail "$n (expected non-zero)"; else pass "$n"; fi; }
assert_rc()   { local n="$1" want="$2"; shift 2; "$@" >/dev/null 2>&1; local got=$?; if [ "$got" = "$want" ]; then pass "$n"; else fail "$n (want rc=$want got=$got)"; fi; }

# --- Stub curl: echoes a canned SonarCloud measures response from STUB_JSON. ---
STUB_DIR="$(mktemp -d)"
cat > "$STUB_DIR/curl" <<'STUB'
#!/usr/bin/env bash
printf '%s' "${STUB_JSON:-}"
STUB
chmod +x "$STUB_DIR/curl"
cleanup() { rm -rf "$STUB_DIR"; }
trap cleanup EXIT

# shellcheck source=../sonar-coverage-guard.sh disable=SC1091
source "$GUARD"

echo "scov_below_floor (float-safe numeric compare):"
assert_ok   "79.95 < 82 is below"            scov_below_floor 79.95 82
assert_ok   "80.0 < 82 is below"             scov_below_floor 80.0 82
assert_ok   "79.95 < 80 is below (the gate)" scov_below_floor 79.95 80
assert_fail "82 is not below 82"             scov_below_floor 82 82
assert_fail "85 is not below 82"             scov_below_floor 85 82
assert_fail "80 is not below 80"             scov_below_floor 80 80
assert_fail "100 is not below 82"            scov_below_floor 100 82

echo
echo "scov_main via NEW_COVERAGE_OVERRIDE:"
export NEW_COVERAGE_FLOOR=82  # read by scov_main in the sourced guard
NEW_COVERAGE_OVERRIDE=79.95 assert_fail "79.95 fails the 82 floor"           scov_main
NEW_COVERAGE_OVERRIDE=80.0  assert_fail "80.0 boundary fails the 82 floor"   scov_main
NEW_COVERAGE_OVERRIDE=81.99 assert_fail "81.99 fails the 82 floor"           scov_main
NEW_COVERAGE_OVERRIDE=82    assert_ok   "82.0 clears the 82 floor"           scov_main
NEW_COVERAGE_OVERRIDE=88.4  assert_ok   "88.4 clears the 82 floor"           scov_main

echo
echo "scov_main via stubbed API (CURL_BIN):"
export CURL_BIN="$STUB_DIR/curl"
export SONAR_TOKEN="dummy"
unset NEW_COVERAGE_OVERRIDE
STUB_JSON='{"component":{"measures":[{"metric":"new_coverage","periods":[{"index":1,"value":"88.0"}]}]}}' \
  assert_ok   "API 88.0 clears the 82 floor" scov_main
STUB_JSON='{"component":{"measures":[{"metric":"new_coverage","periods":[{"index":1,"value":"79.95110024449878"}]}]}}' \
  assert_fail "API 79.95 (CI value) fails"   scov_main
STUB_JSON='{"component":{"measures":[]}}' \
  assert_ok   "no new_coverage metric → skip" scov_main

echo
echo "scov_main prerequisite:"
( unset SONAR_TOKEN; unset NEW_COVERAGE_OVERRIDE; CURL_BIN=/bin/false; assert_rc "no token + no override → rc 2" 2 scov_main )

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
