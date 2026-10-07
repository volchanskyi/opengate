#!/usr/bin/env bash
# Applies the repository's monitoring ConfigMaps to the cluster, then reads them back.
# The read-back is trusted over the apply, and the running store is asked what it loaded.
#
# Environment:
#   TELEGRAM_CHAT_ID             (required) the chat alerts are routed to
#   MONITORING_NAMESPACE         (required) namespace of the monitoring stack, never NAMESPACE
#   MONITORING_RELOAD_INTERVAL   seconds between reloads while the store catches up (default 10)
#   MONITORING_READBACK_WAIT     seconds the panel and rule checks wait for readings (default 120)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
: "${MONITORING_NAMESPACE:?MONITORING_NAMESPACE must name the namespace the monitoring stack runs in}"

ALERTING_DIR="$REPO_ROOT/deploy/grafana/provisioning/alerting"
DASHBOARD_DIR="$REPO_ROOT/deploy/grafana/provisioning/dashboards"
SCRAPE_FILE="$REPO_ROOT/deploy/helm/monitoring/files/vmagent-scrape.yaml"
STREAM_AGGR_FILE="$REPO_ROOT/deploy/helm/monitoring/files/edge-sentinel-stream-aggr.yaml"

READBACK="$SCRIPT_DIR/monitoring-readback.py"
STORE_PROXY="/api/v1/namespaces/$MONITORING_NAMESPACE/services/monitoring-victoriametrics:8428/proxy"
RELOAD_INTERVAL="${MONITORING_RELOAD_INTERVAL:-10}"
# A ConfigMap reaches a mounted file within the kubelet's sync period, about a
# minute, so twelve reloads ten seconds apart outlast it twice.
RELOAD_ATTEMPTS=12
READBACK_WAIT="${MONITORING_READBACK_WAIT:-120}"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

refuse() {
  echo "::error::$1" >&2
  exit 1
}

# Grafana fails to start on an environment-substituted numeric chat id, so it is rendered quoted;
# a value with its own quotes is refused because Telegram rejects the stored id at send time.
chat_id="${TELEGRAM_CHAT_ID:-}"
[ -n "$chat_id" ] || refuse "TELEGRAM_CHAT_ID is not set, so the alerts would be provisioned with nowhere to go."
case "$chat_id" in
  *'"'* | *"'"*) refuse "TELEGRAM_CHAT_ID carries its own quotes, which Grafana stores verbatim and Telegram then rejects." ;;
esac

# Each ConfigMap renders into its own directory, so apply and read-back compare one shape.
mkdir -p "$WORK/grafana-alerting" "$WORK/grafana-dashboards" "$WORK/monitoring-victoriametrics-scrape"

for file in "$ALERTING_DIR"/*.yml; do
  name="$(basename "$file")"
  sed "s|__TELEGRAM_CHAT_ID__|${chat_id}|g" "$file" >"$WORK/grafana-alerting/$name"
done
cp "$DASHBOARD_DIR"/* "$WORK/grafana-dashboards/"
# Both keys belong in the scrape ConfigMap; rendering one would apply it with the other deleted.
cp "$SCRAPE_FILE" "$WORK/monitoring-victoriametrics-scrape/scrape.yml"
cp "$STREAM_AGGR_FILE" "$WORK/monitoring-victoriametrics-scrape/stream-aggr.yml"

live_json() {
  kubectl -n "$MONITORING_NAMESPACE" get configmap "$1" -o json 2>/dev/null || true
}

# The live copy goes in so its labels and annotations come back out; Helm refuses a ConfigMap
# that lost its ownership metadata.
desired_json() {
  python3 "$SCRIPT_DIR/monitoring-config-render.py" "$1" "$WORK/$1" <<<"$(live_json "$1")"
}

# An absent ConfigMap differs from everything.
differs() {
  local name="$1" desired="$2" live
  live="$(live_json "$name")"
  [ -n "$live" ] || return 0
  ! python3 "$SCRIPT_DIR/monitoring-config-render.py" --same "$desired" <<<"$live"
}

changed=()
for name in grafana-alerting grafana-dashboards monitoring-victoriametrics-scrape; do
  desired="$(desired_json "$name")"

  if ! differs "$name" "$desired"; then
    echo "$name: unchanged"
    continue
  fi

  printf '%s' "$desired" | kubectl -n "$MONITORING_NAMESPACE" apply -f - >/dev/null

  # The read-back is the signal: a refused apply can print a warning and still exit zero.
  if differs "$name" "$desired"; then
    refuse "$name did not come back from the cluster as it was applied; the configuration there is not the one declared here."
  fi
  echo "$name: applied"
  changed+=("$name")
done

# Grafana and VictoriaMetrics read their files at start-up; only a changed ConfigMap restarts
# its workload, once however many of its ConfigMaps changed.
readers=()
for name in "${changed[@]:-}"; do
  case "$name" in
    grafana-alerting | grafana-dashboards) readers+=("deployment/monitoring-grafana") ;;
    monitoring-victoriametrics-scrape) readers+=("statefulset/monitoring-victoriametrics") ;;
  esac
done

for workload in $(printf '%s\n' "${readers[@]:-}" | sort -u); do
  kubectl -n "$MONITORING_NAMESPACE" rollout restart "$workload" >/dev/null
  kubectl -n "$MONITORING_NAMESPACE" rollout status "$workload" --timeout=300s >/dev/null
  echo "restarted $workload so it reads what it was just given"
done

if [ "${#changed[@]}" -eq 0 ]; then
  echo "the cluster already holds the monitoring configuration this commit declares."
else
  echo "the cluster now holds the monitoring configuration this commit declares."
fi

# The store is asked what it loaded; an older file is reloaded until the declared one is held
# or the kubelet has had well over its sync period to refresh the mount.
store_loaded() {
  local loaded
  loaded="$(kubectl get --raw "$STORE_PROXY/config")" \
    || refuse "the metrics store could not be asked what scrape configuration it loaded."
  printf '%s' "$loaded" >"$WORK/store-loaded.yml"
  python3 "$READBACK" loaded "$SCRAPE_FILE" <"$WORK/store-loaded.yml"
}

reloads=0
until store_loaded; do
  if [ "$reloads" -ge "$RELOAD_ATTEMPTS" ]; then
    refuse "the metrics store still runs a scrape configuration other than the declared one after $reloads reloads; what it loaded begins: $(head -c 400 "$WORK/store-loaded.yml" | tr '\n' ' ')"
  fi
  kubectl get --raw "$STORE_PROXY/-/reload" >/dev/null \
    || refuse "the metrics store could not be asked to reload its scrape configuration."
  reloads=$((reloads + 1))
  [ "$RELOAD_INTERVAL" = "0" ] || sleep "$RELOAD_INTERVAL"
done
if [ "$reloads" -gt 0 ]; then
  echo "the metrics store was running an older scrape configuration; it loaded the declared one after $reloads reload(s)."
else
  echo "the metrics store runs the declared scrape configuration."
fi

VM_NAMESPACE="$MONITORING_NAMESPACE" python3 "$READBACK" panels "$DASHBOARD_DIR" --wait "$READBACK_WAIT" \
  || refuse "a dashboard panel answers nothing and does not say what empty means; each is named above."
echo "every panel answers, in each environment it is read in."
VM_NAMESPACE="$MONITORING_NAMESPACE" python3 "$READBACK" coverage "$ALERTING_DIR/alert-rules.yml" --wait "$READBACK_WAIT" \
  || refuse "a production alert rule reads a series the store does not hold; each is named above."
echo "every production rule reads a series the store holds."
