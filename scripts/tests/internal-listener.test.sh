#!/usr/bin/env bash
# The server serves two doors, and this holds them apart.
#
# What the world may reach is one chi router behind one catch-all ingress rule,
# so anything mounted on it is published — the exposition renders every series
# the process holds, and the profiler dumps its stacks and its heap. Both moved
# to a second listener the Service publishes and the Ingress does not route.
#
# That boundary is spread across a Go default, a chart values file, a Deployment
# argument, two container ports, a Service port, a scrape job and two compose
# stacks. Any one of them disagreeing is silent: the scrape finds no endpoint,
# or a probe reads the SPA fallback and calls it a pass. So the port is read
# back out of every place that names it, and required to be the same number.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

CHART="$REPO_ROOT/deploy/helm/opengate"
VALUES="$CHART/values.yaml"
DEPLOYMENT="$CHART/templates/server-deployment.yaml"
SERVICE="$CHART/templates/server-service.yaml"
INGRESS="$CHART/templates/ingress.yaml"
SCRAPE="$REPO_ROOT/deploy/helm/monitoring/files/vmagent-scrape.yaml"
LISTENER_GO="$REPO_ROOT/server/internal/app/internal_listener.go"
API_GO="$REPO_ROOT/server/internal/api/api.go"
MAIN_GO="$REPO_ROOT/server/cmd/meshserver/main.go"

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

echo "internal listener:"

# --- the port every place has to agree on -------------------------------------

METRICS_PORT="$(awk '/^[[:space:]]+metricsPort:/ { print $2; exit }' "$VALUES")"
if [[ "$METRICS_PORT" =~ ^[0-9]+$ ]]; then
  pass "the chart names the internal port (server.metricsPort=$METRICS_PORT)"
else
  fail "deploy/helm/opengate/values.yaml must set server.metricsPort"
  METRICS_PORT=""
fi

HTTP_PORT="$(awk '/^[[:space:]]+httpPort:/ { print $2; exit }' "$VALUES")"
if [[ -n "$METRICS_PORT" && "$METRICS_PORT" != "$HTTP_PORT" ]]; then
  pass "the internal port is not the port the ingress routes"
else
  fail "server.metricsPort must differ from server.httpPort, or there is no boundary"
fi

# The binary's own default. A pod spec that stopped passing -internal-listen
# would fall back to it, so a default that disagrees with the chart is a
# listener nothing can reach.
if [ -n "$METRICS_PORT" ] && grep -qF "defaultInternalListen = \":${METRICS_PORT}\"" "$LISTENER_GO"; then
  pass "the binary's default internal address is the chart's port"
else
  fail "server/internal/app/internal_listener.go's default must be :$METRICS_PORT"
fi

if grep -qF '"internal-listen"' "$MAIN_GO"; then
  pass "the binary takes the internal address as a flag"
else
  fail "cmd/meshserver must expose -internal-listen"
fi

# --- the pod serves it and the Service publishes it ---------------------------

if grep -qF '"-internal-listen"' "$DEPLOYMENT" \
  && grep -qF '":{{ .Values.server.metricsPort }}"' "$DEPLOYMENT"; then
  pass "the Deployment starts the internal listener on the chart's port"
else
  fail "the Deployment must pass -internal-listen :{{ .Values.server.metricsPort }}"
fi

if grep -qF 'containerPort: {{ .Values.server.metricsPort }}' "$DEPLOYMENT" \
  && grep -qF 'name: metrics' "$DEPLOYMENT"; then
  pass "the container publishes the internal port as 'metrics'"
else
  fail "the Deployment must declare a container port named 'metrics'"
fi

if grep -qF 'port: {{ .Values.server.metricsPort }}' "$SERVICE" \
  && grep -qF 'targetPort: metrics' "$SERVICE"; then
  pass "the Service publishes the internal port so the cluster can reach it"
else
  fail "the Service must publish a 'metrics' port targeting the container's"
fi

# --- and the edge does not route it -------------------------------------------

if grep -qF 'name: http' "$INGRESS" && ! grep -qF 'name: metrics' "$INGRESS"; then
  pass "the Ingress backs onto the http port only"
else
  fail "the Ingress must not route the internal port"
fi

# The exposition is registered on the internal mux and nowhere else. A route
# re-added to the API router is published by the catch-all rule the moment it
# lands, and nothing else in this file would notice.
if grep -qF '"/metrics"' "$LISTENER_GO"; then
  pass "the exposition is served by the internal listener"
else
  fail "the internal listener must serve /metrics"
fi

if grep -qF '"/metrics"' "$API_GO" || grep -qF '/debug/pprof' "$API_GO"; then
  fail "the public API router registers an internal route — the ingress publishes it"
else
  pass "the public API router registers neither the exposition nor the profiler"
fi

if grep -qF '"/debug/pprof/"' "$LISTENER_GO"; then
  pass "the profiler is served by the internal listener"
else
  fail "the internal listener must serve /debug/pprof/"
fi

# net/http/pprof's init installs the profiler on http.DefaultServeMux, which is
# reachable from anywhere in the binary. Importing it for that side effect is
# how a profiler ends up on a listener nobody chose.
if grep -rqF '_ "net/http/pprof"' "$REPO_ROOT/server"; then
  fail "net/http/pprof is imported for its side effect — it installs itself on DefaultServeMux"
else
  pass "net/http/pprof is registered by hand, not through DefaultServeMux"
fi

# --- the scrape reads the port the Service publishes --------------------------

server_scrape_block() {
  awk '
    index($0, "- job_name: opengate-server") { in_block = 1 }
    in_block && /^  - job_name:/ && !index($0, "- job_name: opengate-server") { exit }
    in_block { print }
  ' "$SCRAPE"
}

if grep -qF 'regex: metrics' <<<"$(server_scrape_block)"; then
  pass "the server scrape job keeps the endpoint port named 'metrics'"
else
  fail "the server scrape job must keep the 'metrics' endpoint port, or it scrapes nothing"
fi

# --- both compose stacks publish it, or the harnesses that read it are blind ---

# Parsed rather than grepped: a command list is equally valid written inline or
# one argument per line, and a gate that recognises only one of those shapes
# fails the next time somebody reformats the file — which says nothing about
# whether the listener is started.
compose_serves_internal_port() {
  python3 - "$1" "$2" <<'PYEOF'
import sys, yaml

path, port = sys.argv[1], sys.argv[2]
with open(path, encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)

server = doc.get("services", {}).get("server", {})

command = server.get("command") or []
if isinstance(command, str):
    command = command.split()
command = [str(word) for word in command]

started = any(
    word == "-internal-listen" and command[i + 1 :][:1] == [f":{port}"]
    for i, word in enumerate(command)
)
published = any(str(mapping) == f"{port}:{port}" for mapping in server.get("ports") or [])

sys.exit(0 if started and published else 1)
PYEOF
}

for compose in deploy/docker-compose.test.yml deploy/docker-compose.perf.yml; do
  file="$REPO_ROOT/$compose"
  if [ -n "$METRICS_PORT" ] && compose_serves_internal_port "$file" "$METRICS_PORT"; then
    pass "$compose starts and publishes the internal listener"
  else
    fail "$compose must start -internal-listen :$METRICS_PORT and publish it"
  fi
done

printf '\nSummary: %d passed, %d failed\n' "$PASS" "$FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf '  - %s\n' "${FAILURES[@]}" >&2
  exit 1
fi
exit 0
