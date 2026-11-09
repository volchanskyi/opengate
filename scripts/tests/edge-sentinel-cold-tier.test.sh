#!/usr/bin/env bash
# Pins the central store to raw readings: no store-side rollup, one concrete keep-time.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CHART_DIR="$REPO_ROOT/deploy/helm/monitoring"
STS_FILE="$CHART_DIR/templates/victoriametrics.yaml"
VALUES_FILE="$CHART_DIR/values.yaml"
APPLY_SCRIPT="$REPO_ROOT/deploy/scripts/monitoring-config-apply.sh"

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

echo "central store keeps raw readings:"

if grep -qF -- '-streamAggr' "$STS_FILE"; then
  fail "the store must take no stream-aggregation flag; charts read raw readings"
else
  pass "the store takes no stream-aggregation flag"
fi

rollup_files="$(find "$CHART_DIR" -name '*stream-aggr*' || true)"
if [ -n "$rollup_files" ]; then
  fail "the chart must ship no rollup file: $rollup_files"
else
  pass "the chart ships no rollup file"
fi

if grep -qF 'stream-aggr' "$STS_FILE" "$APPLY_SCRIPT"; then
  fail "no ConfigMap key or apply step may carry a rollup file"
else
  pass "no ConfigMap key or apply step carries a rollup file"
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
