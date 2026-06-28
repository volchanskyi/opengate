#!/usr/bin/env bash
# Offline regression tests for the Kubernetes VictoriaMetrics scrape config.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SCRAPE_FILE="$REPO_ROOT/deploy/helm/monitoring/files/vmagent-scrape.yaml"

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

job_block() {
  local job="$1"
  awk -v job="$job" '
    index($0, "- job_name: " job) { in_block = 1 }
    in_block && /^  - job_name:/ && !index($0, "- job_name: " job) { exit }
    in_block { print }
  ' "$SCRAPE_FILE"
}

echo "monitoring scrape config:"

pod_block="$(job_block kubernetes-pods)"
if grep -qF 'source_labels: [__meta_kubernetes_pod_ip, __meta_kubernetes_pod_annotation_prometheus_io_port]' <<<"$pod_block" \
  && grep -qF "replacement: \${1}:\${2}" <<<"$pod_block"; then
  pass "annotated pod scrape keeps pod IP when replacing the annotated port"
else
  fail "annotated pod scrape must replace __address__ with pod_ip:annotated_port"
fi

monitoring_block="$(job_block monitoring-service-endpoints)"
if grep -qF 'names: [monitoring]' <<<"$monitoring_block" \
  && grep -qF 'source_labels: [__meta_kubernetes_service_annotation_prometheus_io_scrape]' <<<"$monitoring_block" \
  && grep -qF 'source_labels: [__meta_kubernetes_endpoint_port_name]' <<<"$monitoring_block" \
  && grep -qF 'regex: metrics' <<<"$monitoring_block"; then
  pass "monitoring Service endpoints scrape annotated metrics services"
else
  fail "monitoring Service endpoints job must scrape annotated metrics services"
fi

server_block="$(job_block opengate-server)"
if grep -qF 'source_labels: [__meta_kubernetes_endpoint_port_name]' <<<"$server_block" \
  && grep -qF 'regex: http' <<<"$server_block"; then
  pass "OpenGate server scrape is restricted to the HTTP metrics endpoint"
else
  fail "OpenGate server scrape must not target QUIC/MPS ports"
fi

if grep -qF 'role: node' "$SCRAPE_FILE"; then
  fail "scrape config should use node-exporter instead of direct kubelet node scraping"
else
  pass "scrape config avoids direct kubelet node scraping"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
