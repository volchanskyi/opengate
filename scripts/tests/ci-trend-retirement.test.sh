#!/usr/bin/env bash
# Structural regression guard for the retired CI trend backends.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

fail() {
  printf 'ci-trend-retirement: %s\n' "$1" >&2
  exit 1
}

legacy_files=(
  scripts/lib/loki-push.sh
  scripts/mutation-loki-push.sh
  scripts/pmat-loki-push.sh
  scripts/pmat-loki-query.sh
  scripts/terraform-drift-loki-push.sh
  scripts/tests/loki-transport.test.sh
)

for path in "${legacy_files[@]}"; do
  [ ! -e "$REPO_ROOT/$path" ] || fail "legacy file still exists: $path"
done

if rg -n --glob '!ci-trend-retirement.test.sh' \
  'mutation-loki-push|pmat-loki-(push|query)|terraform-drift-loki-push' \
  "$REPO_ROOT/.github/workflows" "$REPO_ROOT/scripts" >/dev/null; then
  fail "workflow or script still references a legacy Loki trend helper"
fi

if rg -n 'benchmark-action/github-action-benchmark|^[[:space:]]+(go-bench|rust-bench|bench-publish):' \
  "$REPO_ROOT/.github/workflows/ci.yml" >/dev/null; then
  fail "legacy gh-pages benchmark publishing still exists"
fi
rg -q 'rm -rf gh-pages/dev/bench' "$REPO_ROOT/.github/workflows/ci.yml" \
  || fail "gh-pages benchmark data cleanup is not wired into its deployment owner"

rg -q 'pmat-vm-query\.sh repo_score' "$REPO_ROOT/.github/workflows/pmat-trend.yml" \
  || fail "PMAT workflow does not read its previous score from VM"
rg -q 'pmat-vm-query\.sh below_bplus' "$REPO_ROOT/.github/workflows/pmat-trend.yml" \
  || fail "PMAT workflow does not read its previous below-B+ count from VM"

for dashboard in mutation-trend pmat-trend terraform-drift-trend; do
  file="$REPO_ROOT/deploy/grafana/provisioning/dashboards/$dashboard.json"
  jq -e '[.. | objects | .datasource?.type? // empty] | index("loki") | not' "$file" >/dev/null \
    || fail "$dashboard still uses the Loki datasource"
  jq -e '[.. | objects | .datasource?.uid? // empty] | index("VictoriaMetrics") != null' "$file" >/dev/null \
    || fail "$dashboard does not use VictoriaMetrics"
done

[ -f "$REPO_ROOT/deploy/helm/monitoring/templates/loki.yaml" ] \
  || fail "runtime Loki deployment was removed"
[ -f "$REPO_ROOT/deploy/helm/monitoring/files/loki-config.yml" ] \
  || fail "runtime Loki configuration was removed"

printf 'ci-trend-retirement: clean\n'
