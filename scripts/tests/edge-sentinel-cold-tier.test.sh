#!/usr/bin/env bash
# Offline regression tests for the Edge Sentinel long-term (cold) tier: the
# VictoriaMetrics stream-aggregation rollup config and its retention wiring.
#
# The cardinality budget ratified in server/tests/vmcardinality/spike_test.go is
# a pure model — it computes series counts from host profiles and never reads the
# live YAML, so it cannot catch the deployed rollup config drifting away from the
# locked "central = avg only" decision. This test pins the config to that budget:
# every rollup emits exactly one aggregate (avg). min/max/last are agent-local
# (WS-14b), never central rollups, so emitting them would ~4x central active
# series and blow the 50k budget.

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

# Extract a single stream-aggr rule block by its `- name:` value. Blocks start at
# column 0 with `- name:` and run until the next `- name:` (or EOF).
rule_block() {
  awk -v n="$1" '
    $0 == "- name: " n { inb = 1; print; next }
    inb && /^- name:/ { inb = 0 }
    inb { print }
  ' "$AGGR_FILE"
}

echo "edge sentinel cold-tier config:"

# Each rollup interval must exist, target the opengate_edge_ family, and emit
# exactly avg (the locked avg-only central decision).
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

# Defense in depth: no rollup anywhere may emit min/max/last centrally.
if grep -E '^  outputs:' "$AGGR_FILE" | grep -qE '\b(min|max|last)\b'; then
  fail "no central rollup may emit min/max/last — they ~4x active series past the 50k budget"
else
  pass "no central rollup emits min/max/last"
fi

# Raw 10 s input must survive alongside the rollups: -streamAggr.keepInput.
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

# Single global OSS retention window (no per-series split in OSS single-node).
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
