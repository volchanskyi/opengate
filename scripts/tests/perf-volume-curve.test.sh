#!/usr/bin/env bash
# Tests for scripts/perf-volume-curve.sh — the step that reads the volume
# family's legs together.
#
# The family varies how much data is already there and holds the load constant.
# Three bundles a night were produced and nothing compared them: no publish
# step, no trend, no gate. Three runs that happen to share a profile, and the
# whole subject of the family — whether the system slows down as the estate
# grows — is a comparison between those three and nowhere else.
#
# So the cases below are about what a sweep over data volume has to be able to
# say: that its legs held different amounts of data, that the load it held
# constant was actually offered at every one of them, and that they did not all
# come back with the same answer.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CURVE="$REPO_ROOT/scripts/perf-volume-curve.sh"
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

# leg writes one bundle: how many machines the estate ended up holding, what
# that estate weighs, its verdict, what the server took to record a machine, and
# what a technician waited for a fleet list. The last is the half the family
# holds constant, so a leg without one is a leg that offered no technician load
# at all — the fifth argument is left off to produce exactly that.
leg() {
  local devices="$1" verdict="$2" register="$3" bytes="$4" journey="${5:-}"
  local dir="$WORK/legs/volume-$devices"
  mkdir -p "$dir"
  jq -n \
    --argjson devices "$devices" \
    --arg verdict "$verdict" \
    --argjson register "$register" \
    --argjson bytes "$bytes" \
    --arg journey "$journey" \
    '{
      schema_version: 11,
      fixture: { devices: $devices, database_bytes: $bytes },
      observations: [ { series: "register_p95_ms", value: $register } ],
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

echo "perf-volume-curve:"

# --- The shape is published, because the shape is what the family is for ------

reset_legs
leg 500 valid 40 10000000 12
leg 2000 valid 60 30000000 20
leg 8000 valid 396 90000000 48
run_curve
assert_eq "a sweep over three estates is accepted" 0 "$STATUS"
if grep -q '| 500 |' "$WORK/out.txt" && grep -q '| 8000 |' "$WORK/out.txt"; then
  pass "every leg appears in the published table"
else
  fail "the published table does not carry every leg"
  cat "$WORK/out.txt" >&2
fi
if grep -q 'Fleet list p95' "$WORK/out.txt"; then
  pass "the table carries the technician reading the family holds constant"
else
  fail "the table omits the technician reading"
fi

# A curve that fails to rise is not failed. One night is one sample per leg, and
# a gate asserting that more data is always slower would fail the night the
# database got faster at the same size — which is a finding, not a fault.
reset_legs
leg 500 valid 100 10000000 30
leg 2000 valid 40 30000000 12
leg 8000 valid 60 90000000 20
run_curve
assert_eq "a curve that does not rise is published, not failed" 0 "$STATUS"

# --- What it refuses ----------------------------------------------------------

reset_legs
leg 500 valid 40 10000000 12
leg 2000 invalid 60 30000000 20
leg 8000 valid 396 90000000 48
run_curve
assert_eq "a leg that did not measure the system is refused" 1 "$STATUS"
if grep -q 'did not measure' "$WORK/out.txt"; then
  pass "the refusal names the leg that measured nothing"
else
  fail "the refusal does not say which leg measured nothing"
fi

# The legs must hold different amounts of data or the sweep is one point walked
# three times. This is the shape the family actually had before its profiles
# enrolled different fleets: three names, one estate.
reset_legs
leg 500 valid 40 10000000 12
mkdir -p "$WORK/legs/volume-copy"
cp "$WORK/legs/volume-500/bundle.json" "$WORK/legs/volume-copy/bundle.json"
run_curve
assert_eq "legs holding the same estate are refused" 1 "$STATUS"
if grep -q 'distinct' "$WORK/out.txt"; then
  pass "the refusal says the legs are not points on a curve"
else
  fail "the refusal does not explain that the legs are the same point"
fi

# A leg with no technician reading offered nothing but machines arriving, which
# is the load whose cost barely moves with the size of the estate — so the leg
# varied the family's variable against something that cannot feel it.
reset_legs
leg 500 valid 40 10000000 12
leg 2000 valid 60 30000000
run_curve
assert_eq "a leg that offered no technician load is refused" 1 "$STATUS"
if grep -q 'technician' "$WORK/out.txt"; then
  pass "the refusal names the missing technician reading"
else
  fail "the refusal does not mention the technician reading"
fi

# The family's own variable. A leg whose estate was never weighed cannot be
# placed on an axis of how much data is there.
reset_legs
leg 500 valid 40 10000000 12
leg 2000 valid 60 0 20
run_curve
assert_eq "a leg whose estate was never weighed is refused" 1 "$STATUS"
if grep -q 'weigh' "$WORK/out.txt"; then
  pass "the refusal names the missing weight"
else
  fail "the refusal does not mention the weight"
fi

# One leg is a run. Reporting it as a sweep is how a sweep comes to be missing
# two of its three legs with nothing saying so.
reset_legs
leg 500 valid 40 10000000 12
run_curve
assert_eq "a single leg is not a curve" 1 "$STATUS"

# And a directory with nothing in it must not read as a clean sweep.
reset_legs
mkdir -p "$WORK/legs"
run_curve
assert_eq "an empty set of legs is refused" 1 "$STATUS"

STATUS=0
"$CURVE" "$WORK/not-a-directory" >"$WORK/out.txt" 2>&1 || STATUS=$?
assert_eq "a missing directory is refused" 1 "$STATUS"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
