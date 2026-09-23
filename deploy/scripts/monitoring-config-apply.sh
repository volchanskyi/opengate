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
# Environment:
#   TELEGRAM_CHAT_ID  (required) the chat alerts are routed to
#   NAMESPACE                    where the monitoring stack runs (default monitoring)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
NAMESPACE="${NAMESPACE:-monitoring}"

ALERTING_DIR="$REPO_ROOT/deploy/grafana/provisioning/alerting"
DASHBOARD_DIR="$REPO_ROOT/deploy/grafana/provisioning/dashboards"
SCRAPE_FILE="$REPO_ROOT/deploy/helm/monitoring/files/vmagent-scrape.yaml"
STREAM_AGGR_FILE="$REPO_ROOT/deploy/helm/monitoring/files/edge-sentinel-stream-aggr.yaml"

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
restart() {
  kubectl -n "$NAMESPACE" rollout restart "$1" >/dev/null
  echo "restarted $1 so it reads what it was just given"
}

for name in "${changed[@]:-}"; do
  case "$name" in
    grafana-alerting | grafana-dashboards) restart "deployment/monitoring-grafana" ;;
    monitoring-victoriametrics-scrape) restart "deployment/monitoring-victoriametrics" ;;
  esac
done

if [ "${#changed[@]}" -eq 0 ]; then
  echo "the cluster already holds the monitoring configuration this commit declares."
else
  echo "the cluster now holds the monitoring configuration this commit declares."
fi
