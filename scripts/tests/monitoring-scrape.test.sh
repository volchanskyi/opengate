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
  && grep -qF 'regex: metrics' <<<"$server_block"; then
  pass "OpenGate server scrape is restricted to the endpoint that serves the exposition"
else
  fail "OpenGate server scrape must keep the 'metrics' endpoint port and no other"
fi

# The kubelet's cAdvisor endpoint is the only place a container's working set
# against its own limit exists. Nothing else in this cluster publishes it: the
# node exporter reads the node, and a pod at 90% of its cgroup ceiling is
# invisible in a node-wide reading — which is how one sat there for three hours.
cadvisor_block="$(job_block kubernetes-cadvisor)"
if grep -qF 'role: node' <<<"$cadvisor_block" \
  && grep -qF '/metrics/cadvisor' <<<"$cadvisor_block"; then
  pass "the kubelet's cAdvisor endpoint is scraped"
else
  fail "a kubernetes-cadvisor job must scrape the kubelet's /metrics/cadvisor"
fi

# The kubelet serves it over TLS with a certificate signed by the cluster's own
# authority, and it demands the scraper's service-account token.
if grep -qF 'scheme: https' <<<"$cadvisor_block" \
  && grep -qF 'bearer_token_file' <<<"$cadvisor_block" \
  && grep -qF 'ca_file' <<<"$cadvisor_block"; then
  pass "the cAdvisor scrape authenticates to the kubelet over TLS"
else
  fail "the cAdvisor job must present its service-account token to the kubelet over TLS"
fi

# The series are per-container and the alert is per-container, so the namespace,
# pod and container have to survive relabelling.
for label in namespace pod node; do
  if grep -qF "target_label: $label" <<<"$cadvisor_block"; then
    pass "the cAdvisor scrape keeps the $label label"
  else
    fail "the cAdvisor scrape drops the $label label, so an alert cannot name what was killed"
  fi
done

# The node exporter still answers for the node itself; the kubelet job is the
# container's own ceiling, which the node exporter cannot see.
if grep -qF 'job_name: kubernetes-cadvisor' "$SCRAPE_FILE"; then
  pass "the node-level scrape is the kubelet's cAdvisor and nothing wider"
else
  fail "the node role must be used for the cAdvisor job only"
fi

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
