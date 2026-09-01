#!/usr/bin/env bash
# Every gate row must be reachable.
#
# scripts/loadtest-regression-check.sh names its ceilings and floors by
# source/scenario/phase. A row only reaches the gate if
# scripts/loadtest-summarize.sh actually emits that triple from the exports the
# pinned k6 and the QUIC harness write. Those two files have no dependency on
# each other, so a series can be gated by three ceilings while the extraction
# never produces it — a threshold on a series that never arrives reads as a
# passing gate forever.
#
# This test closes that: it reads every triple out of the regression check's own
# case statements and asserts the summarizer produces each one, using the
# **pinned k6 version's** output shape rather than a hand-written schema.
#
# Run: ./scripts/tests/loadtest-gate-series.test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SUMMARIZE="$REPO_ROOT/scripts/loadtest-summarize.sh"
CHECK="$REPO_ROOT/scripts/loadtest-regression-check.sh"
WORKFLOW="$REPO_ROOT/.github/workflows/load-test.yml"

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

for f in "$SUMMARIZE" "$CHECK" "$WORKFLOW"; do
  if [ ! -f "$f" ]; then
    fail "missing file: $f"
    printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
    exit 1
  fi
done

# gated_series — every source/scenario/phase the regression check names in a
# case label. Labels carry an optional trailing metric segment
# (k6/api-baseline/http/* or quic/quic-agents/register/latency_p50_ms); only the
# first three segments identify a series. The `*)` default arms carry no series
# and are skipped by the anchored match.
gated_series() {
  grep -oE '^[[:space:]]*(k6|quic)/[a-z0-9-]+/[a-z0-9]+(/[a-z0-9_()*]+)?\)' "$CHECK" \
    | tr -d ' )' \
    | cut -d/ -f1-3 \
    | sort -u
}

# The fixtures below are the shapes the pinned toolchain writes: k6 v1.x puts a
# metric's statistics flat on the metric object, and the QUIC harness prints its
# own percentile lines. Both are the extraction's real inputs.
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

k6_export >"$WORK/k6/api-baseline.json"
k6_export >"$WORK/k6/concurrent-agents.json"
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
  pass "regression check names at least the eight known series ($gated_count)"
else
  fail "expected >= 8 gated series, found $gated_count — did the case labels change shape?"
fi

# Every gated series must also carry the statistics its ceilings read. A row
# that arrives with only a phase label satisfies the reachability check above
# and still gates nothing.
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

# The fixtures above are only evidence if they match the k6 the workflow pins.
# A version bump that changes the export schema must reach this file too.
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

echo
echo "Summary: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
