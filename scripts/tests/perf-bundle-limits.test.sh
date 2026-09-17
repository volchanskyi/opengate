#!/usr/bin/env bash
# A limit is read off a run that measured something.
#
# Every profile but the staging night's runs where there is no export to join, so
# what their limits are read against is the run's own evidence bundle. That bundle also
# carries the run's verdict about itself, and where the verdict is invalid the
# numbers beside it are readings of something else — the run says so itself, in
# the sentence the verdict step prints.
#
# What it cost: a leg whose fleet count had been refused reported, as its
# headline error, a registration tail of 4.6 seconds against a limit of 500 ms.
# The verdict two steps earlier had already said the run did not measure the
# system. The night before, the same leg on the same profile read 396 ms. So the
# number that named the leg was one the run had already disowned, and the reason
# it went red was two errors further up.
#
# A breach on an invalid run is still printed — every finding stays visible, and
# an invalid run is red on the verdict's account anyway — but it does not decide
# anything. A breach on a run that did measure the system fails, exactly as
# before, and a bundle that cannot be read fails hardest of all: a reader that
# answers yes when it could not ask is the false green this repository rules
# against.
#
# Run: ./scripts/tests/perf-bundle-limits.test.sh

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

# A profile holding one machine-side limit, which is the shape every venue
# without a browser-side generator declares.
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

# bundle writes an evidence bundle carrying a verdict and a registration tail.
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

# A run that measured the system and stayed inside its limits.
bundle valid 396
run_reader
assert_eq "a valid run inside its limits exits 0" "0" "$STATUS"

# A run that measured the system and crossed a limit. This is the case the
# whole thing exists for, and it must stay exactly as it was.
bundle valid 4642
run_reader
assert_eq "a valid run past its limit exits 1" "1" "$STATUS"
if grep -qF "4642" <<<"$(cat "$WORK/out.txt" "$WORK/err.txt")"; then
  pass "and the breach names the reading"
else
  fail "and the breach names the reading"
fi

# The defect. A run the verdict has already disowned still has its numbers
# printed, because a reader who can see them should, but they decide nothing.
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

# A bundle nobody wrote is silence, and silence is not a pass.
rm -f "$WORK/bundle.json"
run_reader
if [ "$STATUS" -ge 2 ]; then
  pass "a bundle that cannot be read fails loudly"
else
  fail "a bundle that cannot be read fails loudly (got=[$STATUS])"
fi

# A bundle carrying no verdict at all is the same thing: whether the run
# measured anything is unknown, and unknown is not permission to judge it.
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
