#!/usr/bin/env bash
# A limit breach decides the outcome only on a run whose verdict says it measured the system.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
READER="$ROOT/scripts/perf-bundle-limits.sh"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

PASS=0
FAIL=0

pass() {
  printf '  PASS %s\n' "$1"
  PASS=$((PASS + 1))
}

fail() {
  printf '  FAIL %s\n' "$1"
  FAIL=$((FAIL + 1))
}

assert_eq() {
  if [ "$2" = "$3" ]; then
    pass "$1"
  else
    fail "$1 (want=[$2] got=[$3])"
  fi
}

cat >"$WORK/profile.yaml" <<'EOF'
schema_version: 1
name: fixture
family: volume
environment: runner
fixture: small

phases:
  - name: steady
    duration: 1m
    operator_arrivals_per_second: 1
    connected_agents: 10
    sessions: 0
    measured: true

safety:
  max_node_memory_percent: 90
  max_error_rate: 0.01

gates:
  - series: quic/quic-agents/register
    metric: latency_p95_ms
    max: 500
    blocking: true
EOF

bundle() {
  local verdict="$1" tail="$2"
  jq -n --arg verdict "$verdict" --argjson tail "$tail" '{
    schema_version: 11,
    run: {commit: "deadbeef", environment: "runner", finished_at: "2026-09-15T12:00:00Z"},
    verdict: {result: $verdict, reasons: (if $verdict == "invalid" then ["the fleet was not there"] else [] end)},
    observations: [
      {series: "register_p95_ms", value: $tail},
      {series: "connect_p95_ms", value: 6},
      {series: "aggregate_error_rate", value: 0}
    ]
  }' >"$WORK/bundle.json"
}

run_reader() {
  STATUS=0
  "$READER" "$WORK/profile.yaml" "$WORK/bundle.json" "$WORK/rows.json" \
    >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
}

echo "perf-bundle-limits:"

bundle valid 396
run_reader
assert_eq "a valid run inside its limits exits 0" "0" "$STATUS"

bundle valid 4642
run_reader
assert_eq "a valid run past its limit exits 1" "1" "$STATUS"
if grep -qF "4642" <<<"$(cat "$WORK/out.txt" "$WORK/err.txt")"; then
  pass "and the breach names the reading"
else
  fail "and the breach names the reading"
fi

bundle invalid 4642
run_reader
assert_eq "an invalid run past its limit exits 0" "0" "$STATUS"
if grep -qF "4642" <<<"$(cat "$WORK/out.txt" "$WORK/err.txt")"; then
  pass "an invalid run still prints what its numbers say"
else
  fail "an invalid run still prints what its numbers say"
fi
if grep -qiE "did not measure|describe nothing|disowned|invalid" <<<"$(cat "$WORK/out.txt" "$WORK/err.txt")"; then
  pass "and says why they decide nothing"
else
  fail "and says why they decide nothing"
fi

rm -f "$WORK/bundle.json"
run_reader
if [ "$STATUS" -ge 2 ]; then
  pass "a bundle that cannot be read fails loudly"
else
  fail "a bundle that cannot be read fails loudly (got=[$STATUS])"
fi

jq -n '{schema_version: 11, run: {}, observations: []}' >"$WORK/bundle.json"
run_reader
if [ "$STATUS" -ge 2 ]; then
  pass "a bundle with no verdict fails loudly"
else
  fail "a bundle with no verdict fails loudly (got=[$STATUS])"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
