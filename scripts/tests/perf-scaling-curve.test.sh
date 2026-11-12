#!/usr/bin/env bash
# Tests for scripts/perf-scaling-curve.sh, which reads every leg of the scaling sweep together.
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

# Leaving off the fifth argument produces a leg that offered no technician load; SIGNIN_P50 and
# SIGNIN_P95 set the machines' sign-in times, inside the profile's ordinary promise by default.
leg() {
  local cpus="$1" verdict="$2" connect="$3" busy="$4" journey="${5:-}"
  local dir="$WORK/legs/scaling-$cpus"
  mkdir -p "$dir"
  jq -n \
    --argjson cpus "$cpus" \
    --arg verdict "$verdict" \
    --argjson connect "$connect" \
    --argjson busy "$busy" \
    --arg journey "$journey" \
    --argjson p50 "${SIGNIN_P50:-3}" \
    --argjson p95 "${SIGNIN_P95:-5}" \
    '{
      schema_version: 11,
      run: { profile_name: "scaling", profile_version: 1 },
      target: { cpus: $cpus, memory_bytes: 1073741824 },
      phases: [ { name: "steady", latency_p95_ms: $connect, target_busy_percent: $busy } ],
      observations: [ { series: "connect_p95_ms", value: $connect },
                      { series: "register_p50_ms", value: $p50 }, { series: "register_p95_ms", value: $p95 } ],
      journeys: (if $journey == "" then []
                 else [ { name: "device-list", latency_p95_ms: ($journey | tonumber) } ] end),
      verdict: { result: $verdict }
    }' >"$dir/bundle.json"
}

reset_legs() { rm -rf "$WORK/legs"; }

run_curve() {
  STATUS=0
  "$CURVE" "$WORK/legs" >"$WORK/out.txt" 2>&1 || STATUS=$?
}

echo "perf-scaling-curve:"

reset_legs
leg 0.25 valid 62 96 310
leg 0.5 valid 41 88 260
leg 1 valid 22 71 190
leg 2 valid 19 44 170
run_curve
assert_eq "a sweep whose legs differ passes" "0" "$STATUS"
if grep -qF '| 0.25 | valid | 62 | 62 | 96 |' "$WORK/out.txt"; then
  pass "the curve names each rung beside what it produced"
else
  fail "the curve names each rung beside what it produced"
fi
if grep -qF 'Sizing figure: 0.25 processors keep the ordinary sign-in promise' "$WORK/out.txt"; then
  pass "a sweep whose every rung keeps the sign-in promise is sized at its smallest rung"
else
  fail "a sweep whose every rung keeps the sign-in promise is sized at its smallest rung ($(cat "$WORK/out.txt"))"
fi

reset_legs
SIGNIN_P50=15387 SIGNIN_P95=51049 leg 0.25 valid 62 96 310
SIGNIN_P50=453 SIGNIN_P95=2438 leg 0.5 valid 41 88 260
leg 1 valid 22 71 190
leg 2 valid 19 44 170
run_curve
if grep -qF '| 0.5 | valid | 41 | 41 | 88 | 260 | 453 / 2438 | missed |' "$WORK/out.txt" \
  && grep -qF '| 1 | valid | 22 | 22 | 71 | 190 | 3 / 5 | kept |' "$WORK/out.txt"; then
  pass "each rung shows its sign-in times and whether they keep the profile's ordinary promise"
else
  fail "each rung shows its sign-in times and whether they keep the profile's ordinary promise ($(cat "$WORK/out.txt"))"
fi
if grep -qF 'Sizing figure: 1 processors keep the ordinary sign-in promise (median within 100 ms, slowest 5% within 500 ms)' "$WORK/out.txt"; then
  pass "the sizing figure is the smallest rung that keeps the promise, its marks read from the profile"
else
  fail "the sizing figure is the smallest rung that keeps the promise ($(cat "$WORK/out.txt"))"
fi
assert_eq "a sweep whose small rungs miss the promise is reported, not failed" "0" "$STATUS"

reset_legs
SIGNIN_P50=900 SIGNIN_P95=3000 leg 1 valid 22 71 190
SIGNIN_P50=800 SIGNIN_P95=2500 leg 2 valid 19 44 170
run_curve
if grep -qF 'Sizing figure: no rung keeps the ordinary sign-in promise' "$WORK/out.txt"; then
  pass "a sweep no rung of which keeps the promise says so"
else
  fail "a sweep no rung of which keeps the promise says so ($(cat "$WORK/out.txt"))"
fi

reset_legs
leg 0.25 valid 62 96 310
leg 0.5 valid 41 88 260
leg 1 valid 22 71 190
leg 2 valid 19 44 170
run_curve
if grep -qF 'Target busy (% of allowance)' "$WORK/out.txt"; then
  pass "the curve publishes how hard the target worked at each rung"
else
  fail "the curve publishes how hard the target worked at each rung"
fi

reset_legs
leg 0.25 valid 16 40 310
leg 0.5 valid 13 41 260
leg 1 valid 18 39 190
leg 2 valid 16 42 170
run_curve
assert_eq "a flat curve is published rather than failed" "0" "$STATUS"

reset_legs
leg 0.25 valid 14 40 190
leg 0.5 valid 14 40 190
leg 1 valid 14 40 190
leg 2 valid 14 40 190
run_curve
if [ "$STATUS" -ne 0 ] && grep -qF 'identical readings' "$WORK/out.txt"; then
  pass "a sweep whose legs all say the same thing fails"
else
  fail "a sweep whose legs all say the same thing fails"
fi

reset_legs
mkdir -p "$WORK/legs/a" "$WORK/legs/b"
leg 1 valid 14 40 190
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

reset_legs
leg 0.25 valid 62 96 310
leg 1 invalid 0 0 190
run_curve
if [ "$STATUS" -ne 0 ] && grep -qF 'did not measure the system' "$WORK/out.txt"; then
  pass "an invalid leg fails the sweep rather than being averaged in"
else
  fail "an invalid leg fails the sweep rather than being averaged in"
fi

reset_legs
leg 1 valid 22 71 190
run_curve
if [ "$STATUS" -ne 0 ] && grep -qF 'at least 2 points' "$WORK/out.txt"; then
  pass "a single leg is refused as a curve"
else
  fail "a single leg is refused as a curve"
fi

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

WORKFLOW="$REPO_ROOT/.github/workflows/perf-scaling.yml"
if grep -qF 'perf-scaling-curve.sh' "$WORKFLOW"; then
  pass "perf-scaling.yml reads its own sweep"
else
  fail "perf-scaling.yml reads its own sweep"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
