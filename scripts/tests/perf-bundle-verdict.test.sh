#!/usr/bin/env bash
# Tests for scripts/perf-bundle-verdict.sh — reading back what the run wrote.
#
# The harness classifies its own run and writes the answer into the evidence
# bundle. Nothing read it: the volume family passed a run whose bundle said
# "invalid" and whose fleet was 0 of 500, and the sweep read as partly working
# when none of it was. A verdict nobody reads is a string in a file.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/scripts/perf-bundle-verdict.sh"
[ -x "$CHECK" ] || {
  echo "FAIL: $CHECK not executable" >&2
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
assert_eq() {
  local name="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

bundle_with() {
  local result="$1"
  shift
  jq -n --arg result "$result" --args '{
    verdict: { result: $result, reasons: $ARGS.positional }
  }' "$@" >"$WORK/bundle.json"
}

run_check() {
  STATUS=0
  "$CHECK" "$WORK/bundle.json" >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
}

echo "perf-bundle-verdict:"

# A run that measured the system and held.
bundle_with valid
run_check
assert_eq "a valid run exits 0" "0" "$STATUS"

# A run that measured the system and breached a gate. Still a measurement: a
# slow night is exactly what the trend exists to record.
bundle_with failed "phase \"steady\" error rate 0.4 is past the ceiling"
run_check
assert_eq "a failed run exits 0, because it measured something" "0" "$STATUS"
if grep -q 'error rate' "$WORK/out.txt"; then
  pass "a failed run's reasons are printed"
else
  fail "a failed run's reasons are printed"
fi

# The defect this exists for.
bundle_with invalid 'scenario "quic-agents" produced no rows, so this run is a partial night'
run_check
assert_eq "a run that measured nothing fails the step" "1" "$STATUS"
if grep -q 'produced no rows' "$WORK/err.txt"; then
  pass "the run's own reason is what the step reports"
else
  fail "the run's own reason is what the step reports"
fi

# A bundle that never arrived is not a passing run. The step runs on every path,
# including the one where the harness died before writing anything, and a guard
# that answers yes when it cannot ask protects nothing.
rm -f "$WORK/bundle.json"
run_check
assert_eq "an absent bundle fails the step" "1" "$STATUS"

# A bundle carrying no verdict is the same absence in a different shape.
echo '{"schema_version":1}' >"$WORK/bundle.json"
run_check
assert_eq "a bundle with no verdict fails the step" "1" "$STATUS"

# So is one that is not JSON at all.
echo 'not json' >"$WORK/bundle.json"
run_check
assert_eq "an unreadable bundle fails the step" "1" "$STATUS"

# A path is required; guessing one would read a bundle from another shard.
STATUS=0
"$CHECK" >/dev/null 2>&1 || STATUS=$?
assert_eq "no argument is a usage error" "2" "$STATUS"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
