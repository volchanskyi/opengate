#!/usr/bin/env bash
# Give the cluster the monitoring configuration the repository declares, and then
# ask for it back.
#
# Three ConfigMaps carry everything Grafana alerts on and everything
# VictoriaMetrics scrapes. Two were created once by hand and re-created by
# nothing; the third is the chart's, and the chart had not been upgraded in over
# a hundred days. So the cluster evaluated seven of the thirteen rules the
# repository declares, served nine of its thirteen dashboards, and scraped none
# of the per-container series — including the ones the rule written to detect
# exactly that gap reads. A rule can be added, pinned by a gate, reviewed,
# merged, and never exist.
#
# The apply is not the guarantee. An apply that lands nothing answers the same
# way as one that lands everything, so what this script trusts is the read-back.
#
# Nor is a ConfigMap the guarantee. It is what a process was given, not what it
# loaded: a relabel sat in the scrape ConfigMap for two days while the running
# store scraped without it, because the chart upgrade that wrote it restarted
# nothing and the nightly apply then found the ConfigMap current. So the store
# is asked what it loaded, reloaded until that is the declared file, and then
# every dashboard panel and every production rule is asked whether it reads
# anything at all.
#
# Environment:
#   TELEGRAM_CHAT_ID  (required) the chat alerts are routed to
#   NAMESPACE                    where the monitoring stack runs (default monitoring)
#   MONITORING_RELOAD_INTERVAL   seconds between reloads while the store catches
#                                up with its ConfigMap (default 10)
#   MONITORING_READBACK_WAIT     how long the panel and rule checks wait for a
#                                target's first readings (default 120)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
NAMESPACE="${NAMESPACE:-monitoring}"

ALERTING_DIR="$REPO_ROOT/deploy/grafana/provisioning/alerting"
DASHBOARD_DIR="$REPO_ROOT/deploy/grafana/provisioning/dashboards"
SCRAPE_FILE="$REPO_ROOT/deploy/helm/monitoring/files/vmagent-scrape.yaml"
STREAM_AGGR_FILE="$REPO_ROOT/deploy/helm/monitoring/files/edge-sentinel-stream-aggr.yaml"

READBACK="$SCRIPT_DIR/monitoring-readback.py"
STORE_PROXY="/api/v1/namespaces/$NAMESPACE/services/monitoring-victoriametrics:8428/proxy"
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

# --- the destination the alerts are routed to ---------------------------------
#
# Grafana provisions a Telegram contact point from a file perfectly well, and its
# environment-variable substitution into the numeric chat-id field is what does
# not work: the value is read back as a JSON number and the server refuses to
# start. So the substitution happens here, into a quoted literal, and what
# reaches the file is a string.
#
# A value carrying its own quotes is the trap worth refusing by name. Grafana
# starts on it and stores the chat id with the quotes included, which Telegram
# rejects at send time — a green apply with a broken destination, and the one
# shape a read-back of the ConfigMap cannot catch.
chat_id="${TELEGRAM_CHAT_ID:-}"
[ -n "$chat_id" ] || refuse "TELEGRAM_CHAT_ID is not set, so the alerts would be provisioned with nowhere to go."
case "$chat_id" in
  *'"'* | *"'"*) refuse "TELEGRAM_CHAT_ID carries its own quotes, which Grafana stores verbatim and Telegram then rejects." ;;
esac

# --- what each ConfigMap should hold ------------------------------------------
#
# Rendered into a directory per ConfigMap, so what is applied and what is read
# back are compared as the same thing: a map of file names to contents.
mkdir -p "$WORK/grafana-alerting" "$WORK/grafana-dashboards" "$WORK/monitoring-victoriametrics-scrape"

for file in "$ALERTING_DIR"/*.yml; do
  name="$(basename "$file")"
  sed "s|__TELEGRAM_CHAT_ID__|${chat_id}|g" "$file" >"$WORK/grafana-alerting/$name"
done
cp "$DASHBOARD_DIR"/* "$WORK/grafana-dashboards/"
# Both keys the scrape ConfigMap carries. Rendering one of them would apply a
# ConfigMap with the other missing, which is a delete wearing an apply's clothes.
cp "$SCRAPE_FILE" "$WORK/monitoring-victoriametrics-scrape/scrape.yml"
cp "$STREAM_AGGR_FILE" "$WORK/monitoring-victoriametrics-scrape/stream-aggr.yml"

# --- apply, then ask for it back ----------------------------------------------

# live_json NAME — what the cluster holds, or nothing when it holds no such
# ConfigMap.
live_json() {
  kubectl -n "$NAMESPACE" get configmap "$1" -o json 2>/dev/null || true
}

# desired_json NAME — the ConfigMap this run wants the cluster to hold. The live
# copy goes in so its labels and annotations come back out: one of these three is
# the chart's, and an apply that drops the ownership Helm recorded is a ConfigMap
# the next upgrade refuses to touch.
desired_json() {
  python3 "$SCRIPT_DIR/monitoring-config-render.py" "$1" "$WORK/$1" <<<"$(live_json "$1")"
}

# differs NAME DESIRED — does the cluster's copy disagree with what we want?
# An absent ConfigMap disagrees with everything.
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

  printf '%s' "$desired" | kubectl -n "$NAMESPACE" apply -f - >/dev/null

  # The read-back. The warning text is not the signal — a refused apply prints
  # one and exits zero, and it changes when the tooling does. What comes back
  # out is the signal.
  if differs "$name" "$desired"; then
    refuse "$name did not come back from the cluster as it was applied; the configuration there is not the one declared here."
  fi
  echo "$name: applied"
  changed+=("$name")
done

# --- the workloads that read them ---------------------------------------------
#
# Grafana reads its provisioning at start-up and VictoriaMetrics reads its scrape
# file the same way, so a ConfigMap that changed has to be picked up. Only one
# that changed: a nightly that restarted Grafana every night would trade a stale
# configuration for a daily outage of the dashboards.
#
# A workload is named by the kind the chart runs it as, and restarted once
# however many of its ConfigMaps changed.
readers=()
for name in "${changed[@]:-}"; do
  case "$name" in
    grafana-alerting | grafana-dashboards) readers+=("deployment/monitoring-grafana") ;;
    monitoring-victoriametrics-scrape) readers+=("statefulset/monitoring-victoriametrics") ;;
  esac
done

for workload in $(printf '%s\n' "${readers[@]:-}" | sort -u); do
  kubectl -n "$NAMESPACE" rollout restart "$workload" >/dev/null
  kubectl -n "$NAMESPACE" rollout status "$workload" --timeout=300s >/dev/null
  echo "restarted $workload so it reads what it was just given"
done

if [ "${#changed[@]}" -eq 0 ]; then
  echo "the cluster already holds the monitoring configuration this commit declares."
else
  echo "the cluster now holds the monitoring configuration this commit declares."
fi

# --- what the running store loaded --------------------------------------------
#
# Asked of the process, never inferred from what this script changed. A store
# on an older file is reloaded, and asked again, until it holds the declared one
# or the kubelet has had well over its sync period to refresh the mounted file.
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

# --- and whether anything reads nothing ---------------------------------------
python3 "$READBACK" panels "$DASHBOARD_DIR" --wait "$READBACK_WAIT" \
  || refuse "a dashboard panel answers nothing and does not say what empty means; each is named above."
echo "every panel answers, in each environment it is read in."
python3 "$READBACK" coverage "$ALERTING_DIR/alert-rules.yml" --wait "$READBACK_WAIT" \
  || refuse "a production alert rule reads a series the store does not hold; each is named above."
echo "every production rule reads a series the store holds."
