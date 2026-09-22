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

# --- what the run's requests were answered with -------------------------------
#
# A night whose presented addresses were not believed fills with refusals and
# reds the error-rate gate, which is exactly what a night against a slow server
# does. The count that separates them is folded into the bundle, and a reading
# nobody prints is a field in an artifact somebody has to know to go and open —
# so the step that reads the verdict reads this beside it.
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
# And a clean night is not warned about. A note that fires on every run is a
# note nobody reads, which is where the reading would end up.
if grep -q '::warning' "$WORK/out.txt"; then
  fail "a run nobody refused must not be warned about, or the note fires every night and says nothing"
else
  pass "a run nobody refused draws no note"
fi

# A bundle whose reading is not a number is one this reader cannot speak about,
# and it must stay quiet rather than end the step it is a passenger on.
jq -n '{
  verdict: { result: "valid", reasons: [] },
  refusals: { requests: "lots", refused: null }
}' >"$WORK/bundle.json"
run_check
assert_eq "an unreadable refusal reading does not fail the step" "0" "$STATUS"

# A run mostly turned away measured the limiter rather than the server, and that
# is a finding about the night's own setup. It is said rather than gated: no
# night of this reading has been taken yet, so a ceiling here would be a number
# nobody has bracketed.
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

# And a venue with no browser-side generator took no such reading, which is not
# a night nobody was refused.
bundle_with valid
run_check
assert_eq "a run that took no such reading still exits 0" "0" "$STATUS"
if grep -qi 'refus' "$WORK/out.txt"; then
  fail "a run that took no refusal reading must not report one"
else
  pass "a run that took no refusal reading reports none"
fi

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
