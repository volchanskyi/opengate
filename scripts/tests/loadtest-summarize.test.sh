#!/usr/bin/env bash
# Tests for scripts/loadtest-summarize.sh load-test trend extraction.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SUMMARIZE="$REPO_ROOT/scripts/loadtest-summarize.sh"
[ -x "$SUMMARIZE" ] || {
  echo "FAIL: $SUMMARIZE not executable" >&2
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
assert_num_eq() {
  local name="$1" want="$2" got="$3"
  if awk -v w="$want" -v g="$got" 'BEGIN { exit !(g == w) }'; then pass "$name"; else fail "$name (want=[$want] got=[$got])"; fi
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/k6"

# The primary fixtures use the pinned k6 shape: v1.x puts statistics flat on the metric object.
cat >"$WORK/k6/api-baseline.json" <<'JSON'
{
  "metrics": {
    "http_req_duration": {
      "avg": 60.1, "min": 12.0, "med": 50.5,
      "p(50)": 50.5, "p(95)": 123.4, "p(99)": 222.2, "max": 300.0
    },
    "http_reqs": { "count": 420, "rate": 42.5 },
    "http_req_failed": { "passes": 418, "fails": 2, "value": 0.005 }
  },
  "root_group": { "name": "", "path": "", "groups": {}, "checks": {} }
}
JSON

cat >"$WORK/k6/relay-throughput.json" <<'JSON'
{
  "metrics": {
    "http_req_duration": {
      "avg": 70.0, "min": 20.0, "med": 63.0,
      "p(50)": 63.0, "p(95)": 140.0, "p(99)": 180.0, "max": 210.0
    },
    "http_reqs": { "count": 75, "rate": 1.25 },
    "http_req_failed": { "passes": 75, "fails": 0, "value": 0 },
    "relay_msg_latency_ms": {
      "avg": 50.0, "min": 20.0, "med": 44.4,
      "p(50)": 44.4, "p(95)": 88.8, "p(99)": 99.9, "max": 120.0
    },
    "relay_msg_count": { "count": 60, "rate": 1.0 }
  },
  "root_group": { "name": "", "path": "", "groups": {}, "checks": {} }
}
JSON

cat >"$WORK/quic.txt" <<'TXT'
Starting QUIC load test: 100 agents → 10.0.0.42:9090

=== Results ===
Total time:  4.9s
Arrival window:  4.9s
Agents:      98/100 succeeded
Failures:    2

Connect:     p50=10ms  p95=750ms  p99=1.5s
Handshake:   p50=20ms  p95=40ms  p99=60ms
Register:    p50=5ms  p95=10ms  p99=15ms

Error samples: 2 failures in 1 kind
  [2x] dial: timeout
TXT

echo "load-test summary extraction:"
OUT="$(
  K6_SUMMARY_DIR="$WORK/k6" QUIC_OUTPUT_FILE="$WORK/quic.txt" GITHUB_SHA="deadbeef" \
    "$SUMMARIZE"
)"
RC=$?
assert_eq "summary exits 0" "0" "$RC"
assert_eq "expected row count" "7" "$(jq 'length' <<<"$OUT")"
assert_num_eq "k6 p95 parsed" "123.4" "$(jq -r '.[] | select(.source=="k6" and .scenario=="api-baseline" and .phase=="http") | .latency_p95_ms' <<<"$OUT")"
assert_num_eq "k6 rps parsed" "42.5" "$(jq -r '.[] | select(.source=="k6" and .scenario=="api-baseline" and .phase=="http") | .rps' <<<"$OUT")"
assert_num_eq "k6 error rate parsed" "0.005" "$(jq -r '.[] | select(.source=="k6" and .scenario=="api-baseline" and .phase=="http") | .error_rate' <<<"$OUT")"

RELAY_ROW="$(jq -r '.[] | select(.source=="k6" and .scenario=="relay-throughput" and .phase=="relay")' <<<"$OUT")"
assert_eq "relay row present under pinned k6 shape" "relay" "$(jq -r '.phase // "MISSING"' <<<"$RELAY_ROW")"
assert_num_eq "relay p50 parsed" "44.4" "$(jq -r '.latency_p50_ms' <<<"$RELAY_ROW")"
assert_num_eq "relay p95 parsed" "88.8" "$(jq -r '.latency_p95_ms' <<<"$RELAY_ROW")"
assert_num_eq "relay p99 parsed" "99.9" "$(jq -r '.latency_p99_ms' <<<"$RELAY_ROW")"
assert_num_eq "relay rps parsed" "1" "$(jq -r '.rps' <<<"$RELAY_ROW")"

assert_num_eq "QUIC p99 duration converted" "1500" "$(jq -r '.[] | select(.source=="quic" and .phase=="connect") | .latency_p99_ms' <<<"$OUT")"
assert_num_eq "QUIC rps computed" "20" "$(jq -r '.[] | select(.source=="quic" and .phase=="aggregate") | .rps' <<<"$OUT")"
assert_num_eq "QUIC error rate computed" "0.02" "$(jq -r '.[] | select(.source=="quic" and .phase=="aggregate") | .error_rate' <<<"$OUT")"
assert_eq "commit tagged" "deadbeef" "$(jq -r '.[0].commit' <<<"$OUT")"

assert_eq "k6 row carries every metric key" \
  "error_rate latency_p50_ms latency_p95_ms latency_p99_ms rps" \
  "$(jq -r '.[] | select(.scenario=="api-baseline") | keys - ["source","scenario","phase","workload","commit","env","timestamp"] | join(" ")' <<<"$OUT")"

for scenario in api-baseline relay-throughput; do
  workload="$(jq -r --arg s "$scenario" '.[] | select(.scenario==$s) | .workload' <<<"$OUT" | sort -u)"
  if [ -n "$workload" ] && [ "$workload" != "null" ] && [ "$(wc -l <<<"$workload")" = "1" ]; then
    pass "k6 $scenario rows declare one workload ($workload)"
  else
    fail "k6 $scenario rows declare no single workload (got [$workload])"
  fi
done

QUIC_WORKLOAD="$(jq -r '.[] | select(.source=="quic") | .workload' <<<"$OUT" | sort -u)"
if [ -n "$QUIC_WORKLOAD" ] && [ "$QUIC_WORKLOAD" != "null" ] && [ "$(wc -l <<<"$QUIC_WORKLOAD")" = "1" ]; then
  pass "QUIC rows declare one workload ($QUIC_WORKLOAD)"
else
  fail "QUIC rows declare no single workload (got [$QUIC_WORKLOAD])"
fi

DISTINCT="$(jq -r '[.[].workload] | unique | length' <<<"$OUT")"
assert_eq "each scenario names its own workload" "3" "$DISTINCT"

mkdir -p "$WORK/k6-undeclared"
cp "$WORK/k6/api-baseline.json" "$WORK/k6-undeclared/brand-new-scenario.json"
rc=0
K6_SUMMARY_DIR="$WORK/k6-undeclared" QUIC_OUTPUT_FILE="$WORK/missing-quic.txt" \
  "$SUMMARIZE" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then
  pass "a scenario with no declared workload is refused"
else
  fail "expected exit 2 for an undeclared scenario, got $rc"
fi

# The harness's clock around the register frame stops at a local send buffer, so registration is
# published only when the server measured it.
cat >"$WORK/no-register-quic.txt" <<'TXT'
Starting QUIC load test: 100 agents across 1 tenant(s) → 10.0.0.42:9090

=== Results ===
Total time:  8m30.12s
Arrival window:  500ms
Agents:      100/100 succeeded
Failures:    0

Connect:     p50=10ms  p95=40ms  p99=60ms
Handshake:   p50=20ms  p95=40ms  p99=60ms
TXT

NOREG_RC=0
NOREG="$(
  K6_SUMMARY_DIR="$WORK/missing-k6" QUIC_OUTPUT_FILE="$WORK/no-register-quic.txt" GITHUB_SHA="deadbeef" \
    "$SUMMARIZE" 2>/dev/null
)" || NOREG_RC=$?
assert_eq "a run without the server's register figure still extracts" "0" "$NOREG_RC"
assert_eq "no register row when the server did not answer" "" \
  "$(jq -r '.[] | select(.phase=="register") | .phase' <<<"$NOREG")"
assert_eq "connect and handshake still published" "connect handshake" \
  "$(jq -r '[.[] | select(.phase=="connect" or .phase=="handshake") | .phase] | sort | join(" ")' <<<"$NOREG")"

cat >"$WORK/no-connect-quic.txt" <<'TXT'
Starting QUIC load test: 100 agents across 1 tenant(s) → 10.0.0.42:9090

=== Results ===
Total time:  8m30.12s
Arrival window:  500ms
Agents:      100/100 succeeded
Failures:    0

Handshake:   p50=20ms  p95=40ms  p99=60ms
TXT
rc=0
K6_SUMMARY_DIR="$WORK/missing-k6" QUIC_OUTPUT_FILE="$WORK/no-connect-quic.txt" \
  "$SUMMARIZE" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then
  pass "a results block missing connect is refused"
else
  fail "expected exit 2 for a results block missing connect, got $rc"
fi

# The QUIC aggregate rate divides machines by the arrival window, since the run's wall clock
# includes the hold.
cat >"$WORK/held-quic.txt" <<'TXT'
Starting QUIC load test: 100 agents across 1 tenant(s) → 10.0.0.42:9090

=== Results ===
Total time:  8m30.12s
Arrival window:  500ms
Agents:      100/100 succeeded
Failures:    0

Connect:     p50=10ms  p95=40ms  p99=60ms
Handshake:   p50=20ms  p95=40ms  p99=60ms
Register:    p50=5ms  p95=10ms  p99=15ms
TXT

HELD="$(
  K6_SUMMARY_DIR="$WORK/missing-k6" QUIC_OUTPUT_FILE="$WORK/held-quic.txt" GITHUB_SHA="deadbeef" \
    "$SUMMARIZE"
)"
HELD_RPS="$(jq -r '.[] | select(.source=="quic" and .phase=="aggregate") | .rps' <<<"$HELD")"
assert_num_eq "held fleet rate divides by the arrival window" "200" "$HELD_RPS"

if awk -v g="$HELD_RPS" 'BEGIN { exit !(g < 1) }'; then
  fail "held fleet rate still divides by the run's wall clock (got $HELD_RPS)"
else
  pass "held fleet rate is not the run's wall clock"
fi

cat >"$WORK/windowless-quic.txt" <<'TXT'
Starting QUIC load test: 100 agents across 1 tenant(s) → 10.0.0.42:9090

=== Results ===
Total time:  8m30.12s
Agents:      100/100 succeeded
Failures:    0

Connect:     p50=10ms  p95=40ms  p99=60ms
Handshake:   p50=20ms  p95=40ms  p99=60ms
Register:    p50=5ms  p95=10ms  p99=15ms
TXT

rc=0
K6_SUMMARY_DIR="$WORK/missing-k6" QUIC_OUTPUT_FILE="$WORK/windowless-quic.txt" \
  "$SUMMARIZE" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then
  pass "a results block with arrivals but no window is refused"
else
  fail "expected exit 2 for a results block with no arrival window, got $rc"
fi

# k6 v0.x nests statistics under a "values" key and exposes a rate metric's ratio as "rate".
mkdir -p "$WORK/k6v0"
cat >"$WORK/k6v0/relay-throughput.json" <<'JSON'
{
  "metrics": {
    "http_req_duration": {
      "type": "trend",
      "contains": "time",
      "values": { "med": 63.0, "p(95)": 140.0, "p(99)": 180.0 }
    },
    "http_reqs": {
      "type": "counter",
      "contains": "default",
      "values": { "count": 75, "rate": 1.25 }
    },
    "http_req_failed": {
      "type": "rate",
      "contains": "default",
      "values": { "rate": 0.005 }
    },
    "relay_msg_latency_ms": {
      "type": "trend",
      "contains": "default",
      "values": { "med": 44.4, "p(95)": 88.8, "p(99)": 99.9 }
    },
    "relay_msg_count": {
      "type": "counter",
      "contains": "default",
      "values": { "count": 60, "rate": 1.0 }
    }
  }
}
JSON

V0OUT="$(
  K6_SUMMARY_DIR="$WORK/k6v0" QUIC_OUTPUT_FILE="$WORK/missing-quic.txt" GITHUB_SHA="deadbeef" \
    "$SUMMARIZE"
)"
V0RELAY="$(jq -r '.[] | select(.phase=="relay")' <<<"$V0OUT")"
assert_num_eq "k6 v0 relay p95 parsed" "88.8" "$(jq -r '.latency_p95_ms' <<<"$V0RELAY")"
assert_num_eq "k6 v0 error rate parsed" "0.005" "$(jq -r '.[] | select(.phase=="http") | .error_rate' <<<"$V0OUT")"
assert_num_eq "k6 v0 p50 falls back to med" "63" "$(jq -r '.[] | select(.phase=="http") | .latency_p50_ms' <<<"$V0OUT")"

# shellcheck source=../loadtest-summarize.sh
source "$SUMMARIZE"
assert_num_eq "sourceable duration converter" "62000" "$(duration_to_ms "1m2s")"

workflow_scenarios="$(
  sed -nE 's/^[[:space:]]*for scenario in ([a-z0-9 -]+); do[[:space:]]*$/\1/p' "$REPO_ROOT/.github/workflows/load-test.yml" \
    | tr ' ' '\n' | grep -v '^$' | sort -u || true
)"
if [ -n "$workflow_scenarios" ]; then
  pass "read $(wc -l <<<"$workflow_scenarios") scenarios from the load-test workflow"
else
  fail "read no scenarios from the load-test workflow's for scenario in list"
fi
declared=0
while IFS= read -r scenario; do
  [ -n "$scenario" ] || continue
  declared=$((declared + 1))
  if workload_name "$scenario" >/dev/null 2>&1; then
    pass "workload declared for $scenario"
  else
    fail "no workload declared for $scenario — it cannot enter the trend"
  fi
done <<<"$workflow_scenarios"$'\n'quic-agents
assert_eq "every scenario's workload is checked" "$(($(wc -l <<<"$workflow_scenarios") + 1))" "$declared"

PARTIAL="$(
  K6_SUMMARY_DIR="$WORK/k6" QUIC_OUTPUT_FILE="$WORK/missing-quic.txt" GITHUB_SHA="deadbeef" \
    "$SUMMARIZE"
)"
assert_eq "partial k6-only extraction succeeds" "3" "$(jq 'length' <<<"$PARTIAL")"

rc=0
K6_SUMMARY_DIR="$WORK/missing-k6" QUIC_OUTPUT_FILE="$WORK/missing-quic.txt" "$SUMMARIZE" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then pass "missing all inputs exits 2"; else fail "missing all inputs expected exit 2, got $rc"; fi

# Stood-down machines never registered, so the succeeded count is short by them without failures.
cat >"$WORK/quic-stood-down.txt" <<'TXT'
=== Results ===
Total time:  5m0s
Arrival window:  2m0s
Agents:      455/500 succeeded
Failures:    1
Stood down:  44

Connect:     p50=10ms  p95=20ms  p99=30ms
Handshake:   p50=1ms  p95=2ms  p99=3ms
TXT

STOOD="$(
  K6_SUMMARY_DIR="$WORK/none" QUIC_OUTPUT_FILE="$WORK/quic-stood-down.txt" GITHUB_SHA="deadbeef" "$SUMMARIZE"
)"
stood_error_rate="$(jq -r '.[] | select(.phase == "aggregate") | .error_rate' <<<"$STOOD")"
# 1 failure out of the 456 machines that actually asked the server for anything.
assert_eq "the stood-down machines leave the error rate" "0.002193" "$stood_error_rate"

stood_rps="$(jq -r '.[] | select(.phase == "aggregate") | .rps' <<<"$STOOD")"
assert_eq "the rate is still the machines that arrived over the window" "3.791667" "$stood_rps"

# A run that replaces machines arrives more than the declared fleet, so no share is computed.
cat >"$WORK/quic-churned.txt" <<'TXT'
=== Results ===
Total time:  4h44m0.996s
Arrival window:  4h30m0.9s
Agents:      2750/500 succeeded
Failures:    0

Connect:     p50=2ms  p95=3ms  p99=4ms
Handshake:   p50=1ms  p95=1ms  p99=2ms
TXT

CHURN_RC=0
CHURN_ERR="$(
  K6_SUMMARY_DIR="$WORK/none" QUIC_OUTPUT_FILE="$WORK/quic-churned.txt" GITHUB_SHA="deadbeef" "$SUMMARIZE" 2>&1 >/dev/null
)" || CHURN_RC=$?
if [ "$CHURN_RC" -eq 2 ]; then
  pass "a run that arrived more machines than it declared refuses"
else
  fail "a run that arrived more machines than it declared expected exit 2, got $CHURN_RC"
fi
if grep -qF -- "more machines" <<<"$CHURN_ERR"; then
  pass "and says the declared fleet is not its denominator"
else
  fail "and says the declared fleet is not its denominator: $CHURN_ERR"
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
