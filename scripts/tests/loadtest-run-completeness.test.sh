#!/usr/bin/env bash
# Tests for scripts/loadtest-run-completeness.sh — the third verdict.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/scripts/loadtest-run-completeness.sh"
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
mkdir -p "$WORK/k6"

rows() {
  local scenarios=("$@")
  {
    printf '['
    local first=1
    for scenario in "${scenarios[@]}"; do
      [ "$first" -eq 1 ] || printf ','
      first=0
      printf '{"source":"k6","scenario":"%s","phase":"http","latency_p95_ms":90}' "$scenario"
    done
    printf ']'
  } >"$WORK/summary.json"
}

run_check() {
  STATUS=0
  LOADTEST_K6_SUMMARY_DIR="$WORK/k6" GITHUB_SHA="deadbeef" \
    "$CHECK" "$WORK/summary.json" "$WORK/completeness.json" >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
}

echo "loadtest-run-completeness:"

# A full night: every scenario produced rows and no mark was breached.
rm -f "$WORK/k6"/*.thresholds
rows api-baseline concurrent-agents relay-throughput quic-agents
run_check
assert_eq "a complete night exits 0" "0" "$STATUS"
assert_eq "a complete night is valid" "valid" "$(jq -r '.result' "$WORK/completeness.json")"
assert_eq "a complete night has nothing missing" "0" "$(jq '.missing_scenarios | length' "$WORK/completeness.json")"

# The defect this exists for: one half ran, the other produced nothing.
rows api-baseline concurrent-agents relay-throughput
run_check
assert_eq "a partial night exits 3" "3" "$STATUS"
assert_eq "a partial night is invalid" "invalid" "$(jq -r '.result' "$WORK/completeness.json")"
assert_eq "a partial night names what is missing" "quic-agents" "$(jq -r '.missing_scenarios | join(",")' "$WORK/completeness.json")"
if grep -q "must not enter the trend" "$WORK/err.txt"; then
  pass "a partial night says it must not enter the trend"
else
  fail "a partial night says it must not enter the trend"
fi

# The record names both halves, so a reader sees what ran rather than inferring
# it from which rows arrived.
assert_eq "the record names what did produce rows" "api-baseline,concurrent-agents,relay-throughput" \
  "$(jq -r '.produced_scenarios | join(",")' "$WORK/completeness.json")"

# Rows from a scenario nobody asked for mean the run measured something other
# than what the profile declared.
rows api-baseline concurrent-agents relay-throughput quic-agents ad-hoc
run_check
assert_eq "an unexpected scenario is invalid" "invalid" "$(jq -r '.result' "$WORK/completeness.json")"
assert_eq "an unexpected scenario is named" "ad-hoc" "$(jq -r '.unexpected_scenarios | join(",")' "$WORK/completeness.json")"

# A breached mark is a failure, which is a measurement — the system was measured
# and it was slow — so it stays in the trend.
rows api-baseline concurrent-agents relay-throughput quic-agents
printf 'api-baseline\n' >"$WORK/k6/api-baseline.thresholds"
run_check
assert_eq "a breached mark exits 0" "0" "$STATUS"
assert_eq "a breached mark is a failure, not an invalid run" "failed" "$(jq -r '.result' "$WORK/completeness.json")"
assert_eq "a breached mark is named" "api-baseline" "$(jq -r '.threshold_breaches | join(",")' "$WORK/completeness.json")"
rm -f "$WORK/k6"/*.thresholds

# A scenario whose every request failed produced a row, and a row is not a
# measurement. The QUIC fleet that could not verify the server's certificate
# emitted rps 0 and error_rate 1 every night for over a week; each of those rows
# counted as the scenario having run, and each one went into the trend.
rowsWithError() {
  local scenario="$1" rate="$2"
  {
    printf '['
    printf '{"source":"k6","scenario":"api-baseline","phase":"http","latency_p95_ms":90,"error_rate":0},'
    printf '{"source":"k6","scenario":"concurrent-agents","phase":"http","latency_p95_ms":90,"error_rate":0},'
    printf '{"source":"k6","scenario":"relay-throughput","phase":"http","latency_p95_ms":90,"error_rate":0},'
    printf '{"source":"quic","scenario":"%s","phase":"aggregate","rps":0,"error_rate":%s}' "$scenario" "$rate"
    printf ']'
  } >"$WORK/summary.json"
}

rowsWithError quic-agents 1
run_check
assert_eq "a scenario where nothing succeeded exits 3" "3" "$STATUS"
assert_eq "a scenario where nothing succeeded is invalid" "invalid" "$(jq -r '.result' "$WORK/completeness.json")"
assert_eq "the scenario that measured nothing is named" "quic-agents" \
  "$(jq -r '.unmeasured_scenarios | join(",")' "$WORK/completeness.json")"

# A run that mostly worked is still a measurement: the error rate is the finding,
# and the trend is where a degrading night belongs.
rowsWithError quic-agents 0.1
run_check
assert_eq "a scenario that mostly worked exits 0" "0" "$STATUS"
assert_eq "a scenario that mostly worked is valid" "valid" "$(jq -r '.result' "$WORK/completeness.json")"

# Which scenarios a run owes is the run's own declaration, so a profile that
# runs fewer of them is not permanently invalid.
rows api-baseline
STATUS=0
LOADTEST_EXPECTED_SCENARIOS="api-baseline" LOADTEST_K6_SUMMARY_DIR="$WORK/k6" \
  "$CHECK" "$WORK/summary.json" "$WORK/completeness.json" >/dev/null 2>&1 || STATUS=$?
assert_eq "a run that owed one scenario and produced it is valid" "0" "$STATUS"

# A missing summary is a setup defect, not a verdict about the system.
STATUS=0
"$CHECK" "$WORK/nope.json" >/dev/null 2>&1 || STATUS=$?
assert_eq "a missing summary exits 2" "2" "$STATUS"

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
