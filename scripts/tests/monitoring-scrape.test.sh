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

# The job reads the production and the staging server alike, and a series with
# nothing naming its environment sums the two: a rule over the server read
# staging's drill as production's rule pack running at five times its ceiling.
# Production's rules filter on the namespace this carries.
if grep -qF 'names: [opengate, opengate-staging]' <<<"$server_block" \
  && grep -qF 'source_labels: [__meta_kubernetes_namespace]' <<<"$server_block" \
  && grep -qF 'target_label: namespace' <<<"$server_block"; then
  pass "every server series names the environment it came from"
else
  fail "the opengate-server job must copy __meta_kubernetes_namespace into a namespace label"
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

# --- and the grant that scrape stands on ---------------------------------------
#
# Reaching each kubelet directly is authorised against one subresource. The
# other one a reader might reach for, nodes/proxy, is what a scrape through the
# API-server proxy would need — it also lets its holder relay arbitrary requests
# through the kubelet, which Trivy refuses as a privilege-escalation path. The
# grant is pinned in both directions so neither half drifts.
RBAC_FILE="$REPO_ROOT/deploy/helm/monitoring/templates/victoriametrics.yaml"

# Comments stripped first: this file explains in prose why one of these grants
# is absent, and a gate that reads the explanation as the grant would fail on
# the very sentence saying it was not made.
rbac_rules() { sed 's/[[:space:]]*#.*$//' "$RBAC_FILE"; }

# Read once into a variable. Piped into `grep -q` the reader stops at its first
# match and pipefail reports the writer's failed write as an absent grant —
# which passes the check below that asserts a grant is absent.
RBAC_RULES="$(rbac_rules)"

if grep -qF 'nodes/metrics' <<<"$RBAC_RULES"; then
  pass "the scraper may read the kubelet's metrics subresource"
else
  fail "the scraper's ClusterRole must grant nodes/metrics, or the cAdvisor scrape is refused"
fi

if grep -qF 'nodes/proxy' <<<"$RBAC_RULES"; then
  fail "the scraper's ClusterRole grants nodes/proxy, which it does not need and which permits privilege escalation"
else
  pass "the scraper holds no node-proxy grant"
fi

# --- a changed configuration restarts what reads it ---------------------------
#
# A chart upgrade that changes a ConfigMap and restarts nothing leaves the
# running process on the old file. The scrape relabel that names each server
# series' environment sat in the ConfigMap for two days that way. Each pod that
# reads its configuration at start carries a checksum of what it mounts, so a
# changed configuration is a changed pod template, and the upgrade rolls it.
checksum_findings="$(
  helm template monitoring "$REPO_ROOT/deploy/helm/monitoring" \
    -f "$REPO_ROOT/deploy/helm/monitoring/values-production.yaml" --set domain=example.invalid 2>/dev/null \
    | python3 -c '
import hashlib, sys, yaml
docs = [d for d in yaml.safe_load_all(sys.stdin) if d]
configmaps = {d["metadata"]["name"]: d.get("data", {}) for d in docs if d["kind"] == "ConfigMap"}
wanted = {"monitoring-victoriametrics", "monitoring-loki", "monitoring-promtail"}
seen = set()
for d in docs:
    if d["kind"] not in ("StatefulSet", "DaemonSet", "Deployment") or d["metadata"]["name"] not in wanted:
        continue
    name = d["metadata"]["name"]
    seen.add(name)
    template = d["spec"]["template"]
    mounted = [v["configMap"]["name"] for v in template["spec"].get("volumes", []) if "configMap" in v]
    text = "".join(configmaps[m][k] for m in mounted for k in sorted(configmaps.get(m, {})))
    want = hashlib.sha256(text.encode()).hexdigest()
    got = (template["metadata"].get("annotations") or {}).get("checksum/config")
    if got != want:
        print(f"{name}: checksum/config is {got}, the configuration it mounts hashes to {want}")
for name in sorted(wanted - seen):
    print(f"{name}: not rendered by the chart")
'
)"
if [ -z "$checksum_findings" ]; then
  pass "VictoriaMetrics, Loki and Promtail carry a checksum of the configuration they mount"
else
  fail "a pod that reads its configuration at start does not restart when it changes: $checksum_findings"
fi

# --- every reading names the environment it came from -------------------------
#
# Production and staging share one store and one set of dashboards. A reading
# with nothing naming its environment is summed with the other one's, so each
# scraped target is reached through a job that copies its namespace and pod.
if grep -qF 'source_labels: [__meta_kubernetes_namespace]' <<<"$pod_block" \
  && grep -qF 'source_labels: [__meta_kubernetes_pod_name]' <<<"$pod_block"; then
  pass "an annotated pod's readings name its namespace and pod"
else
  fail "the kubernetes-pods job must copy the namespace and the pod name onto every reading"
fi

# A deploy replaces the server's pod, and a reading whose only name for the pod
# is its address starts a new line on every deploy with no way to say which
# replica it was.
if grep -qF 'source_labels: [__meta_kubernetes_pod_name]' <<<"$server_block" \
  && grep -qF 'target_label: pod' <<<"$server_block"; then
  pass "every server reading names the pod that served it"
else
  fail "the opengate-server job must copy __meta_kubernetes_pod_name into a pod label"
fi

# Rendered through the charts, so what is checked is what the cluster is given.
render_json() { # chart dir, then helm arguments
  local chart="$1"
  shift
  helm template release "$chart" "$@" 2>/dev/null \
    | python3 -c 'import json, sys, yaml; print(json.dumps([d for d in yaml.safe_load_all(sys.stdin) if d]))'
}
MONITORING_CHART="$REPO_ROOT/deploy/helm/monitoring"
APP_CHART="$REPO_ROOT/deploy/helm/opengate"
command -v helm >/dev/null 2>&1 || fail "helm is not installed, so the charts cannot be rendered"
monitoring_docs="$(render_json "$MONITORING_CHART" -f "$MONITORING_CHART/values-production.yaml" --set domain=example.invalid)"

# The store's own readings — how much disk it holds and how many series — are
# what the soak dashboard watches it grow by. Nothing else publishes them.
vm_scrape="$(jq -r '
  [.[] | select(.kind == "StatefulSet" and (.metadata.name | endswith("victoriametrics")))][0].spec.template as $t
  | ($t.metadata.annotations // {}) as $a
  | [$t.spec.containers[].ports[]? | select(.containerPort == ($a["prometheus.io/port"] // "" | tonumber? // -1))] as $served
  | "\($a["prometheus.io/scrape"] // "absent") \($served | length)"
' <<<"$monitoring_docs")"
if [ "$vm_scrape" = "true 1" ]; then
  pass "VictoriaMetrics' own pod is scraped, on the port it serves"
else
  fail "VictoriaMetrics' pod must carry prometheus.io/scrape and a prometheus.io/port it serves (scrape, ports matched: $vm_scrape)"
fi

# Each database is measured by an exporter in a pod of its own beside it, so
# each environment's readings carry that environment's name and read that
# environment's database. A shared exporter in the monitoring namespace watched
# production alone, labelled its readings as the monitoring stack's, and needed a
# copy of the production database password kept out of band.
for overlay in values-production.yaml values-staging.yaml; do
  app_docs="$(render_json "$APP_CHART" -f "$APP_CHART/$overlay")"
  findings="$(jq -r '
    ([.[] | select(.kind == "StatefulSet")][0]) as $db_set
    | ($db_set.spec.template.spec.containers | map(select(.name == "postgres"))[0]) as $db
    | ([.[] | select(.kind == "Service" and .metadata.name == $db_set.spec.serviceName)][0].metadata.name) as $db_service
    | [.[] | select(.kind == "Deployment") | select(any(.spec.template.spec.containers[]; .image | test("postgres-exporter")))] as $found
    | ($found[0].spec.template // {}) as $t
    | ($t.metadata.annotations // {}) as $a
    | ([$t.spec.containers[]? | select(.image | test("postgres-exporter"))][0] // {}) as $x
    | ([$x.env[]? | select(.valueFrom.secretKeyRef)] | map({(.name): .valueFrom.secretKeyRef.name}) | add // {}) as $secrets
    | ([$db.env[]? | select(.name == "POSTGRES_PASSWORD") | .valueFrom.secretKeyRef.name][0]) as $db_secret
    | ([$x.env[]? | select(.name == "DATA_SOURCE_NAME") | .value][0] // "") as $dsn
    | ($x.image // "" | capture(":v(?<v>[0-9]+\\.[0-9]+\\.[0-9]+)$").v // "0.0.0" | split(".") | map(tonumber)) as $version
    | [
        (if ($found | length) != 1 then "\($found | length) workloads run the exporter, not one" else empty end),
        (if ($db_set.spec.template.spec.containers | length) != 1 then "the database pod runs something beside the database, and its readiness is then the database'"'"'s too" else empty end),
        (if $version < [0, 17, 0] then "the exporter is older than v0.17.0, which cannot read Postgres 17 background-writer view" else empty end),
        (if ($x.args // []) | index("--collector.postmaster") | not then "the postmaster collector is off, so the database start time is never published" else empty end),
        (if ($dsn | contains("@\($db_service):5432/")) | not then "the exporter does not read this environment'"'"'s database (\($dsn), service \($db_service))" else empty end),
        (if $secrets.POSTGRES_PASSWORD != $db_secret then "the exporter reads its password from \($secrets.POSTGRES_PASSWORD // "nowhere"), the database from \($db_secret)" else empty end),
        (if $x.resources.requests.cpu != "5m" or $x.resources.requests.memory != "16Mi" then "the exporter must reserve 5m and 16Mi (got \($x.resources.requests))" else empty end),
        (if ($x.resources.limits.memory // null) == null then "the exporter has no memory ceiling" else empty end),
        (if ($x.livenessProbe | not) or ($x.readinessProbe | not) then "the exporter must carry both health checks" else empty end),
        (if $a["prometheus.io/scrape"] != "true" then "the exporter pod is not annotated for scraping" else empty end),
        (if ([$x.ports[]?.containerPort | tostring] | index($a["prometheus.io/port"] // "")) | not then "the scrape port \($a["prometheus.io/port"] // "absent") is not a port the exporter serves" else empty end)
      ] | .[]
  ' <<<"$app_docs")"
  if [ -z "$findings" ]; then
    pass "$overlay: the database has an exporter of its own beside it, scraped under its namespace"
  else
    fail "$overlay: $(tr '\n' ';' <<<"$findings")"
  fi

  # The server stamps what it writes to the shared store with the environment
  # it runs in, and it learns that from the cluster rather than from a value
  # someone has to keep in step with the namespace it was installed into.
  namespace_source="$(jq -r '
    [.[] | select(.kind == "Deployment" and (.metadata.name | endswith("-server")))][0].spec.template.spec.containers[]
    | select(.name == "server") | .env[] | select(.name == "OPENGATE_NAMESPACE") | .valueFrom.fieldRef.fieldPath // "a literal"
  ' <<<"$app_docs")"
  if [ "$namespace_source" = "metadata.namespace" ]; then
    pass "$overlay: the server learns its environment from the namespace it runs in"
  else
    fail "$overlay: OPENGATE_NAMESPACE must come from the downward API's metadata.namespace (got '${namespace_source:-absent}')"
  fi
done

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
