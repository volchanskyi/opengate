#!/usr/bin/env bash
# Pins the cold-tier rollup config: central rollups emit avg only, since min/max/last stay local.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
AGGR_FILE="$REPO_ROOT/deploy/helm/monitoring/files/edge-sentinel-stream-aggr.yaml"
STS_FILE="$REPO_ROOT/deploy/helm/monitoring/templates/victoriametrics.yaml"
VALUES_FILE="$REPO_ROOT/deploy/helm/monitoring/values.yaml"

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

rule_block() {
  awk -v n="$1" '
    $0 == "- name: " n { inb = 1; print; next }
    inb && /^- name:/ { inb = 0 }
    inb { print }
  ' "$AGGR_FILE"
}

echo "edge sentinel cold-tier config:"

for rule in "edge-sentinel-1m:1m" "edge-sentinel-1h:1h"; do
  name="${rule%%:*}"
  interval="${rule##*:}"
  block="$(rule_block "$name")"

  if [ -z "$block" ]; then
    fail "$name rollup block is missing"
    continue
  fi

  if grep -qF "interval: $interval" <<<"$block"; then
    pass "$name rollup uses the $interval interval"
  else
    fail "$name rollup must declare interval: $interval"
  fi

  if grep -qF 'match: '\''{__name__=~"opengate_edge_.*"}'\''' <<<"$block"; then
    pass "$name rollup matches the opengate_edge_ family"
  else
    fail "$name rollup must match {__name__=~\"opengate_edge_.*\"}"
  fi

  if grep -qE '^  outputs: \[avg\]$' <<<"$block"; then
    pass "$name rollup emits avg only (central cardinality budget)"
  else
    fail "$name rollup must emit outputs: [avg] only — min/max/last are agent-local (WS-14b), never central rollups"
  fi
done

# The outputs lines go to a variable, not a pipe: pipefail reads `grep -q`'s early exit as absence.
aggr_outputs="$(grep -E '^  outputs:' "$AGGR_FILE" || true)"
if grep -qE '\b(min|max|last)\b' <<<"$aggr_outputs"; then
  fail "no central rollup may emit min/max/last — they ~4x active series past the 50k budget"
else
  pass "no central rollup emits min/max/last"
fi

if grep -qF -- '-streamAggr.keepInput' "$STS_FILE"; then
  pass "VictoriaMetrics keeps raw input beside the rollups (-streamAggr.keepInput)"
else
  fail "VictoriaMetrics must pass -streamAggr.keepInput so raw 10 s samples survive"
fi

if grep -qF -- '-streamAggr.config=' "$STS_FILE"; then
  pass "VictoriaMetrics loads the stream-aggregation config"
else
  fail "VictoriaMetrics must load -streamAggr.config"
fi

if grep -qF -- '-retentionPeriod=' "$STS_FILE"; then
  pass "VictoriaMetrics wires a retention period"
else
  fail "VictoriaMetrics must wire -retentionPeriod"
fi

# The OSS single-node build has one global retention window and no per-series split.
if grep -qE '^  retention: [0-9]+[a-z]+$' "$VALUES_FILE"; then
  pass "monitoring values set a concrete VictoriaMetrics retention window"
else
  fail "values.yaml must set victoriametrics.retention to a concrete duration"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
