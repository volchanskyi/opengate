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

# The primary fixtures carry the shape the *pinned* k6 writes: v1.x
# --summary-export puts each metric's statistics flat on the metric object,
# rate metrics expose the ratio as "value", and there is no "values" nesting.
# Extraction is exercised against the schema CI actually feeds it; the v0.x
# shape is kept as a second case below so the summarizer stays tolerant of both.
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

Error samples:
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

# The relay row is the one the gate has three ceilings on. Under the pinned k6
# it must appear from a flat metric object, with every statistic the ceilings
# read — not only when a v0.x "values" nesting happens to be present.
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

# A k6 row that carries no metrics at all is indistinguishable from a scenario
# that never ran, and silently empties the gate. Every metric key must survive.
assert_eq "k6 row carries every metric key" \
  "error_rate latency_p50_ms latency_p95_ms latency_p99_ms rps" \
  "$(jq -r '.[] | select(.scenario=="api-baseline") | keys - ["source","scenario","phase","workload","commit","env","timestamp"] | join(" ")' <<<"$OUT")"

# Every row says which workload produced it.
#
# A trend series compares a number against the numbers before it, and that is
# only sound while the scenario keeps measuring the same thing. A relay scenario
# that had been timing an unauthenticated health check was rewritten to open a
# real session and time its own frame coming back; it kept its name, and the
# first night of the new work was reported as a collapse against the old work's
# figures. Nothing in the stored data could say the two were different.
#
# The name of the workload travels with the sample, so a rewrite is a new series
# and the gate compares it against itself.
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

# A scenario nothing has declared cannot enter the trend: an unnamed workload is
# the ambiguity this label exists to remove, and a row carrying one would be
# compared against whatever else happened to be unnamed.
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

# Registration is published only when the server measured it. The harness's own
# clock around the register frame stops at a local send buffer, so a run the
# server did not answer has nothing to say about registration — and a row of
# zeroes under registration's name is what two gate ceilings sat on for months.
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

# Connect and handshake are the generator's own side of the wire, so their
# absence is a malformed block rather than an optional field.
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

# The QUIC aggregate rate is machines divided by a duration, and the run's own
# wall clock is the wrong one. A run holds its fleet connected for the relay
# generator beside it, so the clock is eight minutes of holding after a second
# of arriving; dividing by it reports the hold. The harness prints the window
# the fleet arrived in, and that is the denominator.
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

# The failure this guards is silent: a rate computed from the run's length is a
# number, just not a rate, and it reads as a collapsed fleet forever.
if awk -v g="$HELD_RPS" 'BEGIN { exit !(g < 1) }'; then
  fail "held fleet rate still divides by the run's wall clock (got $HELD_RPS)"
else
  pass "held fleet rate is not the run's wall clock"
fi

# A results block that reports arrivals but no window leaves the rate with no
# denominator it can trust. Falling back to the run's clock is how the defect
# above would return without anything saying so, so the extraction refuses.
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

# k6 v0.x nested each metric's statistics under a "values" key and exposed a
# rate metric's ratio as "rate". The summarizer accepts that shape too, so the
# extraction does not depend on the exporter generation.
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

# The declaration has to cover every scenario the workflow runs, or the run that
# adds one is refused at extraction rather than pushing an unnamed series.
while IFS= read -r scenario; do
  [ -n "$scenario" ] || continue
  if workload_name "$scenario" >/dev/null 2>&1; then
    pass "workload declared for $scenario"
  else
    fail "no workload declared for $scenario — it cannot enter the trend"
  fi
done < <(
  grep -oE 'loadtest-k6-run\.sh [a-z-]+' "$REPO_ROOT/.github/workflows/load-test.yml" | awk '{print $2}' | sort -u
  echo quic-agents
)

PARTIAL="$(
  K6_SUMMARY_DIR="$WORK/k6" QUIC_OUTPUT_FILE="$WORK/missing-quic.txt" GITHUB_SHA="deadbeef" \
    "$SUMMARIZE"
)"
assert_eq "partial k6-only extraction succeeds" "3" "$(jq 'length' <<<"$PARTIAL")"

rc=0
K6_SUMMARY_DIR="$WORK/missing-k6" QUIC_OUTPUT_FILE="$WORK/missing-quic.txt" "$SUMMARIZE" >/dev/null 2>&1 || rc=$?
if [ "$rc" -eq 2 ]; then pass "missing all inputs exits 2"; else fail "missing all inputs expected exit 2, got $rc"; fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
