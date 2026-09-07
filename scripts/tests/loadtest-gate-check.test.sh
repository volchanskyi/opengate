#!/usr/bin/env bash
# Tests for scripts/loadtest-gate-check.sh — the profile's own numbers deciding
# whether a night was acceptable.
#
# Every profile declares limits, keyed by the same source/scenario/phase triple
# the canonical rows carry. Nothing read them: the schema checked they were
# well-formed and then no code consumed one, so every limit in all seven
# profiles was decoration, including the ones marked as failing the run.
#
# They are read here, in the step where both halves of the night have been
# joined — the browser-side rows and the machine-side rows in one file. Inside
# the harness they could not be: it holds phases and machines and no
# browser-side row at all, so a limit naming one could never have been reached
# from there.
#
# A limit on a measurement that never arrives is the case worth pinning. It
# reads as a passing limit forever, which is the same false green a step that
# reports success for refused work produces.
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

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# A profile carrying one limit that fails the night and one mark that only
# reports, on the same measurement — which is the arrangement the two files of
# numbers were reconciled into.
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

# Canonical rows in the shape the summarizer writes them.
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

# A night inside every limit.
write_profile
write_rows 80 25
run_check
assert_eq "a night inside every limit passes" "0" "$STATUS"
assert_eq "and records no breach" "0" "$(jq 'length' "$WORK/breaches.json")"

# Past the mark that only reports, inside the one that fails. The mark is
# reported so the target it represents is being watched; the night still passes.
write_rows 150 25
run_check
assert_eq "past a reporting mark alone, the night passes" "0" "$STATUS"
assert_eq "and the mark is recorded" "1" "$(jq 'length' "$WORK/breaches.json")"
assert_contains "the mark says it only reports" "reported" "$OUT"

# Past the limit that fails the night. Both it and the mark below it are past,
# so both are recorded and the night fails.
write_rows 260 25
run_check
assert_eq "past an enforced limit, the night fails" "1" "$STATUS"
assert_eq "both the limit and the mark are recorded" "2" "$(jq 'length' "$WORK/breaches.json")"
assert_contains "the breach names the measurement" "k6/api-baseline/http" "$OUT"
assert_contains "the breach names the number it passed" "200" "$OUT"

# A floor, not a ceiling: throughput below what the profile asked for.
write_rows 80 4
run_check
assert_eq "below a floor, the night fails" "1" "$STATUS"
assert_contains "the breach names the floor" "quic/quic-agents/aggregate" "$OUT"

# The case this whole check exists for. A limit on a measurement that never
# arrives reads as a passing limit forever — the same false green as a step
# that reports success for work it was refused.
jq -n '[
  { source: "quic", scenario: "quic-agents", phase: "aggregate", rps: 25, error_rate: 0 }
]' >"$WORK/summary.json"
run_check
assert_eq "a limit whose measurement never arrived fails the night" "1" "$STATUS"
assert_contains "and says the measurement was absent" "never arrived" "$OUT"

# A row that arrived without the number the limit is about is the same absence
# wearing a row's clothing.
jq -n '[
  { source: "k6", scenario: "api-baseline", phase: "http", error_rate: 0 },
  { source: "quic", scenario: "quic-agents", phase: "aggregate", rps: 25, error_rate: 0 }
]' >"$WORK/summary.json"
run_check
assert_eq "a row carrying no such number fails the night" "1" "$STATUS"

# A profile with no limits at all is a profile that decides nothing, which must
# not read as a night that cleared everything.
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

# Inputs that are not there fail rather than passing quietly.
STATUS=0
"$CHECK" "$WORK/nope.yaml" "$WORK/summary.json" >/dev/null 2>&1 || STATUS=$?
assert_eq "an absent profile fails" "2" "$STATUS"

write_profile
STATUS=0
"$CHECK" "$WORK/profile.yaml" "$WORK/nope.json" >/dev/null 2>&1 || STATUS=$?
assert_eq "an absent summary fails" "2" "$STATUS"

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'Failures:\n' >&2
  for f in "${FAILURES[@]}"; do printf '  - %s\n' "$f" >&2; done
  exit 1
fi
