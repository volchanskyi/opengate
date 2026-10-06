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

# Leaving off the fifth argument produces a leg that offered no technician load.
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
    '{
      schema_version: 11,
      target: { cpus: $cpus, memory_bytes: 1073741824 },
      phases: [ { name: "steady", latency_p95_ms: $connect, target_busy_percent: $busy } ],
      observations: [ { series: "connect_p95_ms", value: $connect } ],
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
