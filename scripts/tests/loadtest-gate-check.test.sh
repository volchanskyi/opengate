#!/usr/bin/env bash
# Tests for scripts/loadtest-gate-check.sh: profile limits decide whether a night passed.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHECK="$REPO_ROOT/scripts/loadtest-gate-check.sh"
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
assert_contains() {
  local name="$1" needle="$2" hay="$3"
  case "$hay" in
    *"$needle"*) pass "$name" ;;
    *) fail "$name (missing [$needle] in [$hay])" ;;
  esac
}

assert_lacks() {
  local name="$1" needle="$2" hay="$3"
  case "$hay" in
    *"$needle"*) fail "$name (found [$needle] in [$hay])" ;;
    *) pass "$name" ;;
  esac
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

write_profile() {
  cat >"$WORK/profile.yaml" <<'YAML'
schema_version: 1
name: normal
family: normal
environment: staging
fixture: small
phases:
  - name: steady
    duration: 2m
    connected_agents: 500
safety:
  max_node_cpu_percent: 85
  max_node_memory_percent: 90
  max_error_rate: 0.01
gates:
  - series: k6/api-baseline/http
    metric: latency_p95_ms
    max: 200
    blocking: true
  - series: k6/api-baseline/http
    metric: latency_p95_ms
    max: 100
    blocking: false
  - series: quic/quic-agents/aggregate
    metric: rps
    min: 10
    blocking: true
YAML
}

write_rows() {
  local api_p95="$1" quic_rps="$2"
  jq -n --argjson p95 "$api_p95" --argjson rps "$quic_rps" '[
    { source: "k6", scenario: "api-baseline", phase: "http",
      latency_p95_ms: $p95, error_rate: 0 },
    { source: "quic", scenario: "quic-agents", phase: "aggregate",
      rps: $rps, error_rate: 0 }
  ]' >"$WORK/summary.json"
}

run_check() {
  STATUS=0
  "$CHECK" "$WORK/profile.yaml" "$WORK/summary.json" "$WORK/breaches.json" \
    >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
  OUT="$(cat "$WORK/out.txt" "$WORK/err.txt")"
}

echo "loadtest-gate-check:"

write_profile
write_rows 80 25
run_check
assert_eq "a night inside every limit passes" "0" "$STATUS"
assert_eq "and records no breach" "0" "$(jq 'length' "$WORK/breaches.json")"

write_rows 150 25
run_check
assert_eq "past a reporting mark alone, the night passes" "0" "$STATUS"
assert_eq "and the mark is recorded" "1" "$(jq 'length' "$WORK/breaches.json")"
assert_eq "and recorded as reported only" "false" "$(jq '.[0].enforced' "$WORK/breaches.json")"
assert_contains "the mark says it only reports" "reported" "$OUT"

write_rows 260 25
run_check
assert_eq "past an enforced limit, the night fails" "1" "$STATUS"
assert_eq "both the limit and the mark are recorded" "2" "$(jq 'length' "$WORK/breaches.json")"
assert_eq "and the limit alone is recorded as enforced" "1" \
  "$(jq '[.[] | select(.enforced == true)] | length' "$WORK/breaches.json")"
assert_contains "the breach names the measurement" "k6/api-baseline/http" "$OUT"
assert_contains "the breach names the number it passed" "200" "$OUT"

write_rows 80 4
run_check
assert_eq "below a floor, the night fails" "1" "$STATUS"
assert_contains "the breach names the floor" "quic/quic-agents/aggregate" "$OUT"

jq -n '[
  { source: "quic", scenario: "quic-agents", phase: "aggregate", rps: 25, error_rate: 0 }
]' >"$WORK/summary.json"
run_check
assert_eq "a limit whose measurement never arrived fails the night" "1" "$STATUS"
assert_contains "and says the measurement was absent" "no row for it ever arrived" "$OUT"
assert_lacks "without saying the opposite of the finding" "never arrived" "$OUT"

jq -n '[
  { source: "k6", scenario: "api-baseline", phase: "http", error_rate: 0 },
  { source: "quic", scenario: "quic-agents", phase: "aggregate", rps: 25, error_rate: 0 }
]' >"$WORK/summary.json"
run_check
assert_eq "a row carrying no such number fails the night" "1" "$STATUS"
assert_contains "and says the row arrived without the number" "row that arrived carries no such number" "$OUT"

cat >"$WORK/profile.yaml" <<'YAML'
schema_version: 1
name: bare
family: normal
environment: staging
fixture: small
phases:
  - name: steady
    duration: 2m
    connected_agents: 500
safety:
  max_node_cpu_percent: 85
  max_node_memory_percent: 90
  max_error_rate: 0.01
YAML
write_rows 80 25
run_check
assert_eq "a profile that declares no limits is refused" "2" "$STATUS"

STATUS=0
"$CHECK" "$WORK/nope.yaml" "$WORK/summary.json" >/dev/null 2>&1 || STATUS=$?
assert_eq "an absent profile fails" "2" "$STATUS"

write_profile
STATUS=0
"$CHECK" "$WORK/profile.yaml" "$WORK/nope.json" >/dev/null 2>&1 || STATUS=$?
assert_eq "an absent summary fails" "2" "$STATUS"

SCALING="$REPO_ROOT/load/profiles/scaling.yaml"
[ -f "$SCALING" ] || {
  echo "FAIL: $SCALING missing" >&2
  exit 1
}

scaling_rows() {
  jq -n --argjson rate "$1" '[
    { source: "quic", scenario: "quic-agents", phase: "aggregate",
      error_rate: $rate, latency_p95_ms: 55, rps: 25 },
    { source: "quic", scenario: "quic-agents", phase: "connect",
      latency_p95_ms: 55, error_rate: $rate }
  ]' >"$WORK/summary.json"
}

run_scaling() {
  STATUS=0
  "$CHECK" "$SCALING" "$WORK/summary.json" "$WORK/breaches.json" \
    >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
  OUT="$(cat "$WORK/out.txt" "$WORK/err.txt")"
}

scaling_rows 0
run_scaling
assert_eq "scaling: a leg every machine reached passes" "0" "$STATUS"

scaling_rows 0.00055
run_scaling
assert_eq "scaling: the 09-18 reading (1 machine in 2000) passes" "0" "$STATUS"
scaling_rows 0.0010065425264217413
run_scaling
assert_eq "scaling: the 09-19 reading (2 machines in 2000) passes" "0" "$STATUS"

scaling_rows 0.05
run_scaling
assert_eq "scaling: a leg that stopped working fails" "1" "$STATUS"
assert_contains "scaling: the breach names the arrivals" "quic/quic-agents/aggregate" "$OUT"

SCALING_READ="$(python3 "$SCRIPT_DIR/fixtures/scaling-error-gate.py" "$SCALING")"
SCALING_GATE="$(cut -f1 <<<"$SCALING_READ")"
SCALING_SAFETY="$(cut -f2 <<<"$SCALING_READ")"
if [ -n "$SCALING_GATE" ] && awk -v g="$SCALING_GATE" -v s="$SCALING_SAFETY" 'BEGIN { exit !(g > 0 && g <= s) }'; then
  pass "scaling: the verdict sits inside the run's own write-off line ($SCALING_GATE <= $SCALING_SAFETY)"
else
  fail "scaling: the verdict sits inside the run's own write-off line (gate=[$SCALING_GATE] safety=[$SCALING_SAFETY])"
fi

SOAK="$REPO_ROOT/load/profiles/soak.yaml"
[ -f "$SOAK" ] || {
  echo "FAIL: $SOAK missing" >&2
  exit 1
}

soak_rows() {
  jq -n --argjson p95 "$1" '[
    { source: "quic", scenario: "quic-agents", phase: "aggregate",
      error_rate: 0, latency_p95_ms: 20, rps: 25 },
    { source: "quic", scenario: "quic-agents", phase: "connect",
      latency_p95_ms: $p95, error_rate: 0 }
  ]' >"$WORK/summary.json"
}

run_soak() {
  STATUS=0
  "$CHECK" "$SOAK" "$WORK/summary.json" "$WORK/breaches.json" \
    >"$WORK/out.txt" 2>"$WORK/err.txt" || STATUS=$?
  OUT="$(cat "$WORK/out.txt" "$WORK/err.txt")"
}

soak_rows 2
run_soak
assert_eq "soak: the night that established the ceiling passes" "0" "$STATUS"

soak_rows 4000
run_soak
assert_eq "soak: a night past the ceiling fails the run" "1" "$STATUS"
assert_contains "soak: the breach names the connect path" "quic/quic-agents/connect" "$OUT"
assert_lacks "soak: and does not merely report it" "reported, not enforced" "$OUT"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
