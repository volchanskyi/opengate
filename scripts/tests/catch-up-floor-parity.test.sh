#!/usr/bin/env bash
# Holds the catch-up floor to one value: the server's, the agent's and each store's keep-time.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SERVER_FILE="$REPO_ROOT/server/internal/agentapi/conn_backfill.go"
AGENT_FILE="$REPO_ROOT/agent/crates/mesh-agent-core/src/ml/backfill/mod.rs"
MONITORING_VALUES="$REPO_ROOT/deploy/helm/monitoring/values.yaml"
RELEASE_VALUES="$REPO_ROOT/deploy/helm/opengate/values.yaml"

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

# days_of turns "N * 24 * 3600" into N, and anything else into nothing.
days_of() {
  sed -nE 's/^[^0-9]*([0-9]+) \* 24 \* 3600[^0-9]*$/\1/p' <<<"$1"
}

# store_days reads the keep-time in days that the block named $2 sets in the values file $1.
store_days() {
  awk -v block="$2:" '
    $0 == block { inb = 1; next }
    inb && /^[^ #]/ { inb = 0 }
    inb && $1 == "retention:" { print $2; exit }
  ' "$1" | sed -nE 's/^([0-9]+)d$/\1/p'
}

echo "catch-up floor parity:"

server="$(days_of "$(grep -E '^\s*backfillRetentionSecs\s*=' "$SERVER_FILE" || true)")"
agent="$(days_of "$(grep -E '^\s*retention_secs:' "$AGENT_FILE" || true)")"
monitoring="$(store_days "$MONITORING_VALUES" victoriametrics)"
release="$(store_days "$RELEASE_VALUES" metricsStore)"

for reading in "server constant:$server" "agent default:$agent" \
  "monitoring store keep-time:$monitoring" "release store keep-time:$release"; do
  if [ -n "${reading#*:}" ]; then
    pass "the ${reading%%:*} reads ${reading#*:} days"
  else
    fail "the ${reading%%:*} could not be read as a whole number of days"
  fi
done

if [ -n "$server" ] && [ "$server" = "$agent" ] && [ "$agent" = "$monitoring" ] && [ "$monitoring" = "$release" ]; then
  pass "server, agent and both stores keep catch-up to the same $server days"
else
  fail "the floor differs: server $server, agent $agent, monitoring store $monitoring, release store $release"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
