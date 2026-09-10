#!/usr/bin/env bash
# Tests for scripts/perf-scaling-curve.sh — the step that reads every leg of the
# scaling sweep together.
#
# The sweep existed for months with no consumer: two jobs that read none of each
# other's output, no publish step, no gate, and uploads set to warn on an empty
# file set. Four bundles a night, never compared. So the cases below are about
# the two things a sweep has to be able to say — that its rungs were different
# rungs, and that they did not all come back with the same answer — and about
# the shape it publishes for a reader, which is the reason the sweep runs.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CURVE="$REPO_ROOT/scripts/perf-scaling-curve.sh"
[ -x "$CURVE" ] || {
  echo "FAIL: $CURVE not executable" >&2
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

# leg writes one bundle: the processor share the server was given, its verdict,
# the wait time the machines saw, and how hard the server worked.
leg() {
  local cpus="$1" verdict="$2" connect="$3" busy="$4"
  local dir="$WORK/legs/scaling-$cpus"
  mkdir -p "$dir"
  jq -n \
    --argjson cpus "$cpus" \
    --arg verdict "$verdict" \
    --argjson connect "$connect" \
    --argjson busy "$busy" \
    '{
      schema_version: 5,
      target: { cpus: $cpus, memory_bytes: 1073741824 },
      phases: [ { name: "steady", latency_p95_ms: $connect, target_busy_percent: $busy } ],
      observations: [ { series: "connect_p95_ms", value: $connect } ],
      verdict: { result: $verdict }
    }' >"$dir/bundle.json"
}

reset_legs() { rm -rf "$WORK/legs"; }

run_curve() {
  STATUS=0
  "$CURVE" "$WORK/legs" >"$WORK/out.txt" 2>&1 || STATUS=$?
}

echo "perf-scaling-curve:"

# A sweep whose rungs differ is a curve, whichever way it happens to bend.
reset_legs
leg 0.25 valid 62 96
leg 0.5 valid 41 88
leg 1 valid 22 71
leg 2 valid 19 44
run_curve
assert_eq "a sweep whose legs differ passes" "0" "$STATUS"
if grep -qF '| 0.25 | valid | 62 | 62 | 96 |' "$WORK/out.txt"; then
  pass "the curve names each rung beside what it produced"
else
  fail "the curve names each rung beside what it produced"
fi
if grep -qF 'Target busy (% of allowance)' "$WORK/out.txt"; then
  pass "the curve publishes how hard the target worked at each rung"
else
  fail "the curve publishes how hard the target worked at each rung"
fi

# A curve that does not rise is a finding and not a failure. Two nights from the
# same code disagreed about the shape, so one night's monotonicity is not
# something to gate on.
reset_legs
leg 0.25 valid 16 40
leg 0.5 valid 13 41
leg 1 valid 18 39
leg 2 valid 16 42
run_curve
assert_eq "a flat curve is published rather than failed" "0" "$STATUS"

# The condition the 2026-09-05 sweep was actually in: four legs, one answer.
reset_legs
leg 0.25 valid 14 40
leg 0.5 valid 14 40
leg 1 valid 14 40
leg 2 valid 14 40
run_curve
if [ "$STATUS" -ne 0 ] && grep -qF 'identical readings' "$WORK/out.txt"; then
  pass "a sweep whose legs all say the same thing fails"
else
  fail "a sweep whose legs all say the same thing fails"
fi

# The other half of that night: every leg reporting the same processor share, so
# the rungs were never rungs.
reset_legs
mkdir -p "$WORK/legs/a" "$WORK/legs/b"
leg 1 valid 14 40
cp "$WORK/legs/scaling-1/bundle.json" "$WORK/legs/a/bundle.json"
jq '.observations[0].value = 22 | .phases[0].latency_p95_ms = 22' \
  "$WORK/legs/scaling-1/bundle.json" >"$WORK/legs/b/bundle.json"
rm -rf "$WORK/legs/scaling-1"
run_curve
if [ "$STATUS" -ne 0 ] && grep -qF 'distinct processor shares' "$WORK/out.txt"; then
  pass "a sweep whose legs name one processor share fails"
else
  fail "a sweep whose legs name one processor share fails"
fi

# A leg that measured nothing takes its rung out of the curve, and a curve with
# a hole in it is not one.
reset_legs
leg 0.25 valid 62 96
leg 1 invalid 0 0
run_curve
if [ "$STATUS" -ne 0 ] && grep -qF 'did not measure the system' "$WORK/out.txt"; then
  pass "an invalid leg fails the sweep rather than being averaged in"
else
  fail "an invalid leg fails the sweep rather than being averaged in"
fi

# One rung is a run. Reporting it as a sweep is how three rungs go missing with
# nothing saying so.
reset_legs
leg 1 valid 22 71
run_curve
if [ "$STATUS" -ne 0 ] && grep -qF 'at least 2 points' "$WORK/out.txt"; then
  pass "a single leg is refused as a curve"
else
  fail "a single leg is refused as a curve"
fi

# Nothing at all is the loudest case and the easiest to pass by accident: an
# absence satisfies an absence-shaped check.
reset_legs
mkdir -p "$WORK/legs"
run_curve
if [ "$STATUS" -ne 0 ] && grep -qF 'no leg of the sweep left a bundle' "$WORK/out.txt"; then
  pass "a sweep that produced no bundles fails"
else
  fail "a sweep that produced no bundles fails"
fi

STATUS=0
"$CURVE" "$WORK/nowhere" >"$WORK/out.txt" 2>&1 || STATUS=$?
if [ "$STATUS" -ne 0 ] && grep -qF 'there is no directory' "$WORK/out.txt"; then
  pass "a missing download directory fails rather than reading as an empty sweep"
else
  fail "a missing download directory fails rather than reading as an empty sweep"
fi

# The workflow has to actually run this, or the sweep goes back to having no
# consumer — which is the defect, not the script's absence.
WORKFLOW="$REPO_ROOT/.github/workflows/perf-stack.yml"
if grep -qF 'perf-scaling-curve.sh' "$WORKFLOW"; then
  pass "perf-stack.yml reads its own sweep"
else
  fail "perf-stack.yml reads its own sweep"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
