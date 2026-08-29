#!/usr/bin/env bash
# Tests for scripts/sonar-coverage-guard.sh. Plain bash; no network — the
# new_coverage value is injected via NEW_COVERAGE_OVERRIDE or a stubbed CURL_BIN.
# Run: ./scripts/tests/sonar-coverage-guard.test.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GUARD="$SCRIPT_DIR/../sonar-coverage-guard.sh"
[ -f "$GUARD" ] || {
  echo "FAIL: $GUARD not found" >&2
  exit 1
}

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
assert_ok() {
  local n="$1"
  shift
  if "$@" >/dev/null 2>&1; then pass "$n"; else fail "$n (expected 0, got $?)"; fi
}
assert_fail() {
  local n="$1"
  shift
  if "$@" >/dev/null 2>&1; then fail "$n (expected non-zero)"; else pass "$n"; fi
}
assert_rc() {
  local n="$1" want="$2"
  shift 2
  "$@" >/dev/null 2>&1
  local got=$?
  if [ "$got" = "$want" ]; then pass "$n"; else fail "$n (want rc=$want got=$got)"; fi
}

# --- Stub curl: echoes a canned SonarCloud measures response from STUB_JSON. ---
STUB_DIR="$(mktemp -d)"
cat >"$STUB_DIR/curl" <<'STUB'
#!/usr/bin/env bash
printf '%s' "${STUB_JSON:-}"
STUB
chmod +x "$STUB_DIR/curl"
# Invoked through the EXIT trap.
# shellcheck disable=SC2329
cleanup() { rm -rf "$STUB_DIR"; }
trap cleanup EXIT

# shellcheck source=../sonar-coverage-guard.sh disable=SC1091
source "$GUARD"

echo "scov_below_floor (float-safe numeric compare):"
assert_ok "79.95 < 82 is below" scov_below_floor 79.95 82
assert_ok "80.0 < 82 is below" scov_below_floor 80.0 82
assert_ok "79.95 < 80 is below (the gate)" scov_below_floor 79.95 80
assert_fail "82 is not below 82" scov_below_floor 82 82
assert_fail "85 is not below 82" scov_below_floor 85 82
assert_fail "80 is not below 80" scov_below_floor 80 80
assert_fail "100 is not below 82" scov_below_floor 100 82

echo
echo "scov_main via NEW_COVERAGE_OVERRIDE:"
export NEW_COVERAGE_FLOOR=82 # read by scov_main in the sourced guard
NEW_COVERAGE_OVERRIDE=79.95 assert_fail "79.95 fails the 82 floor" scov_main
NEW_COVERAGE_OVERRIDE=80.0 assert_fail "80.0 boundary fails the 82 floor" scov_main
NEW_COVERAGE_OVERRIDE=81.99 assert_fail "81.99 fails the 82 floor" scov_main
NEW_COVERAGE_OVERRIDE=82 assert_ok "82.0 clears the 82 floor" scov_main
NEW_COVERAGE_OVERRIDE=88.4 assert_ok "88.4 clears the 82 floor" scov_main

echo
echo "scov_main via stubbed API (CURL_BIN):"
export CURL_BIN="$STUB_DIR/curl"
export SONAR_TOKEN="dummy"
unset NEW_COVERAGE_OVERRIDE
STUB_JSON='{"component":{"measures":[{"metric":"new_coverage","periods":[{"index":1,"value":"88.0"}]}]}}' \
  assert_ok "API 88.0 clears the 82 floor" scov_main
STUB_JSON='{"component":{"measures":[{"metric":"new_coverage","periods":[{"index":1,"value":"79.95110024449878"}]}]}}' \
  assert_fail "API 79.95 (CI value) fails" scov_main
STUB_JSON='{"component":{"measures":[]}}' \
  assert_ok "no new_coverage metric → skip" scov_main

echo
echo "scov_main prerequisite:"
(
  unset SONAR_TOKEN
  unset NEW_COVERAGE_OVERRIDE
  CURL_BIN=/bin/false
  assert_rc "no token + no override → rc 2" 2 scov_main
)

echo
echo "scov_added_lines (what a diff actually touched):"
assert_lines() {
  local name="$1" want="$2" diff="$3" got
  got="$(scov_added_lines <<<"$diff" | tr '\n' ' ')"
  if [ "$got" = "$want" ]; then pass "$name"; else fail "$name (want [$want] got [$got])"; fi
}
assert_lines "a multi-line hunk reports every line it added" "12 13 14 " \
  "@@ -10,2 +12,3 @@ func x() {"
# A hunk with no count is one line. Reading the absent count as zero loses it,
# and a one-line change is the commonest shape there is.
assert_lines "a single-line hunk with no count reports that line" "7 " \
  "@@ -7 +7 @@"
# A pure deletion touches no line of the working tree, so it adds nothing that
# needs covering — counting it would demand coverage of a line that is not there.
assert_lines "a pure deletion adds nothing to cover" "" \
  "@@ -4,3 +3,0 @@"
assert_lines "several hunks are all reported" "2 9 10 " \
  "$(printf '@@ -2 +2 @@\n@@ -8,0 +9,2 @@\n')"

echo
echo "scov_changed_lines (a file git has never seen):"
WORK="$(mktemp -d)"
# Invoked through the EXIT trap.
# shellcheck disable=SC2329
cleanup_work() { rm -rf "$WORK"; }
trap 'cleanup; cleanup_work' EXIT
printf 'a\nb\nc\n' >"$WORK/new.go"
untracked="$(cd "$WORK" && bash -c "source '$GUARD'; scov_changed_lines new.go" | tr '\n' ' ')"
if [ "$untracked" = "1 2 3 " ]; then
  pass "a file with no history counts every line"
else
  fail "a file with no history counts every line (got [$untracked])"
fi

echo
echo "scov_check_diff (blame-independent, per changed line):"
export SCOV_CHANGED_OVERRIDE="server/internal/app/background.go"
export SCOV_SETTLE_RETRIES=0
export SCOV_SETTLE_SLEEP=0

# The failure this half exists for: a file carved out of another arrives with
# every line dated to the split, so CI measures all of it as new code. Four of
# nine changed lines hit is 44%, which is roughly what the gate saw.
split_lines() {
  local hit="$1" miss="$2" out="" i
  for ((i = 1; i <= hit; i++)); do out+="server/internal/app/background.go:$i:3"$'\n'; done
  for ((i = hit + 1; i <= hit + miss; i++)); do out+="server/internal/app/background.go:$i:0"$'\n'; done
  printf '%s' "$out"
}
touched_lines() {
  local n="$1" out="" i
  for ((i = 1; i <= n; i++)); do out+="server/internal/app/background.go:$i"$'\n'; done
  printf '%s' "$out"
}
export SCOV_TOUCHED_OVERRIDE
SCOV_TOUCHED_OVERRIDE="$(touched_lines 9)"

SCOV_LINES_OVERRIDE="$(split_lines 4 5)" \
  assert_rc "a split file at 44% is refused" 1 scov_check_diff
SCOV_LINES_OVERRIDE="$(split_lines 9 0)" \
  assert_ok "the same file fully covered passes" scov_check_diff
SCOV_LINES_OVERRIDE="$(split_lines 8 1)" \
  assert_ok "88% clears the 82 floor" scov_check_diff

# A line the coverage report says nothing about — a comment, a blank, a
# declaration — is a line nothing has to cover, and counting it as uncovered
# would demand a test for a line no test can reach.
SCOV_LINES_OVERRIDE="$(split_lines 2 0)" \
  assert_ok "lines with no coverage figure do not count against the ratio" scov_check_diff

# A guard that answers yes when it could not ask is the false green it was
# written to close.
SCOV_LINES_OVERRIDE="other/file.go:1:5" \
  assert_rc "no figures for any changed file → rc 2, never a pass" 2 scov_check_diff

unset SCOV_CHANGED_OVERRIDE SCOV_TOUCHED_OVERRIDE SCOV_SETTLE_RETRIES SCOV_SETTLE_SLEEP

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
