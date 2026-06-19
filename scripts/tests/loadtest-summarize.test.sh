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

cat >"$WORK/k6/api-baseline.json" <<'JSON'
{
  "metrics": {
    "http_req_duration": {
      "type": "trend",
      "contains": "time",
      "values": { "med": 50.5, "p(95)": 123.4, "p(99)": 222.2 }
    },
    "http_reqs": {
      "type": "counter",
      "contains": "default",
      "values": { "count": 420, "rate": 42.5 }
    },
    "http_req_failed": {
      "type": "rate",
      "contains": "default",
      "values": { "rate": 0.005 }
    }
  }
}
JSON

cat >"$WORK/k6/relay-throughput.json" <<'JSON'
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
      "values": { "rate": 0 }
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

cat >"$WORK/quic.txt" <<'TXT'
Starting QUIC load test: 100 agents → 10.0.0.42:9090

=== Results ===
Total time:  4.9s
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
assert_num_eq "custom relay latency parsed" "88.8" "$(jq -r '.[] | select(.source=="k6" and .scenario=="relay-throughput" and .phase=="relay") | .latency_p95_ms' <<<"$OUT")"
assert_num_eq "QUIC p99 duration converted" "1500" "$(jq -r '.[] | select(.source=="quic" and .phase=="connect") | .latency_p99_ms' <<<"$OUT")"
assert_num_eq "QUIC rps computed" "20" "$(jq -r '.[] | select(.source=="quic" and .phase=="aggregate") | .rps' <<<"$OUT")"
assert_num_eq "QUIC error rate computed" "0.02" "$(jq -r '.[] | select(.source=="quic" and .phase=="aggregate") | .error_rate' <<<"$OUT")"
assert_eq "commit tagged" "deadbeef" "$(jq -r '.[0].commit' <<<"$OUT")"

# shellcheck source=../loadtest-summarize.sh
source "$SUMMARIZE"
assert_num_eq "sourceable duration converter" "62000" "$(duration_to_ms "1m2s")"

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
