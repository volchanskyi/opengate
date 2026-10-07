#!/usr/bin/env bash
# Every profile limit reaches an emitted measurement, and every emitted measurement has a decision.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SUMMARIZE="$REPO_ROOT/scripts/loadtest-summarize.sh"
PROFILE="$REPO_ROOT/load/profiles/normal.yaml"
WORKFLOW="$REPO_ROOT/.github/workflows/load-test.yml"
# shellcheck source=scripts/lib/loadtest-profile.sh
. "$REPO_ROOT/scripts/lib/loadtest-profile.sh"

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

echo "loadtest gate-series reachability:"

for f in "$SUMMARIZE" "$PROFILE" "$WORKFLOW"; do
  if [ ! -f "$f" ]; then
    fail "missing file: $f"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

gated_series() {
  profile_gates "$PROFILE" | jq -r '.[].series' | sort -u
}

decided_measurements() {
  {
    profile_gates "$PROFILE" | jq -r '.[] | "\(.series)|\(.metric)"'
    profile_ungated "$PROFILE" | jq -r '.[] | "\(.series)|\(.metric)"'
  } | sort -u
}

emitted_measurements() {
  jq -r '
    .[]
    | . as $row
    | ["latency_p50_ms", "latency_p95_ms", "latency_p99_ms", "rps", "error_rate", "dropped_iterations"][]
    | select($row[.] != null)
    | "\($row.source)/\($row.scenario)/\($row.phase)|\(.)"
  ' <<<"$1" | sort -u
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/k6"

k6_export() {
  local extra="${1:-}"
  cat <<JSON
{
  "metrics": {
    "http_req_duration": {
      "avg": 40.0, "min": 10.0, "med": 35.0,
      "p(50)": 35.0, "p(95)": 90.0, "p(99)": 120.0, "max": 150.0
    },
    "http_reqs": { "count": 900, "rate": 15.0 },
    "http_req_failed": { "passes": 900, "fails": 0, "value": 0 }${extra}
  },
  "root_group": { "name": "", "path": "", "groups": {}, "checks": {} }
}
JSON
}

DROPPED=',
    "dropped_iterations": { "count": 0, "rate": 0.0 }'

k6_export "$DROPPED"',
    "journey_device_list_ms": {
      "avg": 45.0, "min": 12.0, "med": 40.0,
      "p(50)": 40.0, "p(95)": 88.0, "p(99)": 120.0, "max": 150.0
    },
    "journey_device_detail_ms": {
      "avg": 70.0, "min": 20.0, "med": 60.0,
      "p(50)": 60.0, "p(95)": 140.0, "p(99)": 200.0, "max": 260.0
    },
    "journey_command_accept_ms": {
      "avg": 110.0, "min": 30.0, "med": 95.0,
      "p(50)": 95.0, "p(95)": 240.0, "p(99)": 400.0, "max": 520.0
    }' >"$WORK/k6/api-baseline.json"
k6_export "$DROPPED" >"$WORK/k6/concurrent-agents.json"
k6_export ',
    "relay_msg_latency_ms": {
      "avg": 30.0, "min": 8.0, "med": 25.0,
      "p(50)": 25.0, "p(95)": 60.0, "p(99)": 80.0, "max": 95.0
    },
    "relay_msg_count": { "count": 1200, "rate": 20.0 }' >"$WORK/k6/relay-throughput.json"

cat >"$WORK/quic.txt" <<'TXT'
Starting QUIC load test: 100 agents across 1 tenant(s) → 10.0.0.42:9090

=== Results ===
Total time:  1.5s
Arrival window:  1.5s
Agents:      100/100 succeeded
Failures:    0

Connect:     p50=10ms  p95=40ms  p99=60ms
Handshake:   p50=20ms  p95=40ms  p99=60ms
Register:    p50=5ms  p95=10ms  p99=15ms
TXT

ROWS="$(
  K6_SUMMARY_DIR="$WORK/k6" QUIC_OUTPUT_FILE="$WORK/quic.txt" GITHUB_SHA="gate" \
    "$SUMMARIZE"
)"

produced="$(jq -r '.[] | "\(.source)/\(.scenario)/\(.phase)"' <<<"$ROWS" | sort -u)"

gated_count=0
while IFS= read -r series; do
  [ -n "$series" ] || continue
  gated_count=$((gated_count + 1))
  if grep -qxF "$series" <<<"$produced"; then
    pass "gated series is produced: $series"
  else
    fail "gated series is never produced by the extraction: $series"
  fi
done < <(gated_series)

if [ "$gated_count" -ge 8 ]; then
  pass "the profile names at least the eight known series ($gated_count)"
else
  fail "expected >= 8 gated series, found $gated_count — did the profile's gates change shape?"
fi

undecided=""
decided="$(decided_measurements)"
while IFS= read -r measurement; do
  [ -n "$measurement" ] || continue
  grep -qxF "$measurement" <<<"$decided" || undecided="$undecided $measurement"
done < <(emitted_measurements "$ROWS")

if [ -z "$undecided" ]; then
  pass "every measurement the extraction produces carries a decision"
else
  fail "measurements nobody ruled on (limit them, or declare them ungated with a reason):$undecided"
fi

emitted="$(emitted_measurements "$ROWS")"
phantom=""
while IFS= read -r measurement; do
  [ -n "$measurement" ] || continue
  grep -qxF "$measurement" <<<"$emitted" || phantom="$phantom $measurement"
done < <(decided_measurements)

if [ -z "$phantom" ]; then
  pass "every decision the profile records names a measurement that arrives"
else
  fail "decisions about measurements the extraction never produces:$phantom"
fi

while IFS= read -r series; do
  [ -n "$series" ] || continue
  src="${series%%/*}"
  rest="${series#*/}"
  scen="${rest%%/*}"
  phase="${rest#*/}"
  keys="$(jq -r --arg s "$src" --arg c "$scen" --arg p "$phase" \
    '[.[] | select(.source==$s and .scenario==$c and .phase==$p) | keys[]]
     | map(select(. == "latency_p50_ms" or . == "latency_p95_ms" or . == "rps" or . == "error_rate"))
     | length' <<<"$ROWS")"
  if [ "${keys:-0}" -ge 2 ]; then
    pass "gated series carries gate statistics: $series"
  else
    fail "gated series carries no gate statistics: $series"
  fi
done < <(gated_series)

PINNED="$(grep -oE 'K6_VERSION:[[:space:]]*v[0-9]+\.[0-9]+\.[0-9]+' "$WORKFLOW" | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -n1)"
if [ -n "$PINNED" ]; then
  pass "workflow pins a k6 version ($PINNED)"
else
  fail "workflow pins no K6_VERSION — the fixture shape cannot be tied to a release"
fi
case "$PINNED" in
  v1.*) pass "fixtures encode the pinned k6 major (v1 flat-statistics shape)" ;;
  *) fail "pinned k6 is $PINNED but the fixtures here encode the v1 flat-statistics shape" ;;
esac

# The harness reports a registration tail past the last finite histogram bucket as that boundary.
# A limit at or above it reads a number that cannot rise, so it can never fail.
BUCKETS_FILE="$REPO_ROOT/server/internal/metrics/registration_pool.go"
if [ ! -f "$BUCKETS_FILE" ]; then
  fail "the registration histogram's buckets are not where the limits are checked against them"
else
  buckets_line="$(grep -E '^var registrationDurationBuckets' "$BUCKETS_FILE" || true)"
  bounds="$(tr -d '{} ' <<<"${buckets_line#*\{}")"
  top_bucket_ms="$(awk -F, '{ printf "%d", $NF * 1000 }' <<<"$bounds")"
  if [ -z "$top_bucket_ms" ] || [ "$top_bucket_ms" -le 0 ]; then
    fail "could not read the registration histogram's last boundary out of $BUCKETS_FILE"
  else
    pass "the registration histogram's last boundary is ${top_bucket_ms}ms"
    swept=0
    over=""
    while IFS= read -r profile; do
      [ -n "$profile" ] || continue
      while IFS='|' read -r series metric max; do
        [ -n "$series" ] || continue
        case "$series" in */register) ;; *) continue ;; esac
        case "$metric" in latency_*) ;; *) continue ;; esac
        [ "$max" != "null" ] || continue
        swept=$((swept + 1))
        if awk -v m="$max" -v top="$top_bucket_ms" 'BEGIN { exit !(m >= top) }'; then
          over="$over $(basename "$profile") $series $metric=$max"
        fi
      done < <(profile_gates "$profile" | jq -r '.[] | "\(.series)|\(.metric)|\(.max)"')
    done < <(find "$REPO_ROOT/load/profiles" -name '*.yaml' | sort)

    if [ -z "$over" ]; then
      pass "every registration limit sits under it"
    else
      fail "registration limits at or above the last boundary, where the reading cannot rise to meet them:$over"
    fi
    if [ "$swept" -ge 3 ]; then
      pass "swept $swept registration limit(s)"
    else
      fail "swept only $swept registration limit(s) — did the profiles stop naming them?"
    fi
  fi
fi

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
