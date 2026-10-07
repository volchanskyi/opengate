#!/usr/bin/env bash
# Tests for scripts/perf-bundle-verdict.sh, which reads the verdict a run wrote into its bundle.
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

bundle_with valid
run_check
assert_eq "a valid run exits 0" "0" "$STATUS"

bundle_with failed "phase \"steady\" error rate 0.4 is past the ceiling"
run_check
assert_eq "a failed run exits 0, because it measured something" "0" "$STATUS"
if grep -q 'error rate' "$WORK/out.txt"; then
  pass "a failed run's reasons are printed"
else
  fail "a failed run's reasons are printed"
fi

bundle_with invalid 'scenario "quic-agents" produced no rows, so this run is a partial night'
run_check
assert_eq "a run that measured nothing fails the step" "1" "$STATUS"
if grep -q 'produced no rows' "$WORK/err.txt"; then
  pass "the run's own reason is what the step reports"
else
  fail "the run's own reason is what the step reports"
fi

rm -f "$WORK/bundle.json"
run_check
assert_eq "an absent bundle fails the step" "1" "$STATUS"

echo '{"schema_version":1}' >"$WORK/bundle.json"
run_check
assert_eq "a bundle with no verdict fails the step" "1" "$STATUS"

echo 'not json' >"$WORK/bundle.json"
run_check
assert_eq "an unreadable bundle fails the step" "1" "$STATUS"

jq -n '{
  verdict: { result: "valid", reasons: [] },
  refusals: { requests: 115228, refused: 0 }
}' >"$WORK/bundle.json"
run_check
assert_eq "a run nobody refused still exits 0" "0" "$STATUS"
if grep -q '115228' "$WORK/out.txt" && grep -qi 'refus' "$WORK/out.txt"; then
  pass "what the run asked for and what it was refused are both printed"
else
  fail "the refusal reading is printed beside the verdict"
fi
if grep -q '::warning' "$WORK/out.txt"; then
  fail "a run nobody refused must not be warned about, or the note fires every night and says nothing"
else
  pass "a run nobody refused draws no note"
fi

jq -n '{
  verdict: { result: "valid", reasons: [] },
  refusals: { requests: "lots", refused: null }
}' >"$WORK/bundle.json"
run_check
assert_eq "an unreadable refusal reading does not fail the step" "0" "$STATUS"

jq -n '{
  verdict: { result: "failed", reasons: ["phase \"steady\" error rate 0.42 is past the ceiling"] },
  refusals: { requests: 40000, refused: 32000 }
}' >"$WORK/bundle.json"
run_check
assert_eq "a run refused at the door is still a run that reported" "0" "$STATUS"
if grep -qi 'address' "$WORK/out.txt" || grep -qi 'address' "$WORK/err.txt"; then
  pass "a run mostly turned away says the presented addresses may not have been believed"
else
  fail "a run mostly turned away must name the presented addresses as the thing to check"
fi

bundle_with valid
run_check
assert_eq "a run that took no such reading still exits 0" "0" "$STATUS"
if grep -qi 'refus' "$WORK/out.txt"; then
  fail "a run that took no refusal reading must not report one"
else
  pass "a run that took no refusal reading reports none"
fi

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
