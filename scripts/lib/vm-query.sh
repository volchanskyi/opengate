#!/usr/bin/env bash
# Reads the in-cluster VictoriaMetrics Service through a throwaway kubectl curl pod.
# Transport, empty and parse failures print nothing and exit 0, so a build never fails on them.
#
# Usage:
#   vm_query_nightly <metric> <selector> <dates>
#   vm_nightly_window <metric> <selector> <dates>
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
#   VM_NAMESPACE  namespace of the Service (default monitoring)
#   VM_SERVICE  the Service name (default monitoring-victoriametrics)
#   VM_CURL_IMAGE  the curl pod image (default docker.io/curlimages/curl:8.11.1)

# How far back the store keeps anything (the chart's -retentionPeriod).
VM_RETENTION_DAYS=30

vm_tonight() {
  local started="${VM_RUN_STARTED_AT:-}"
  if [[ ! "$started" =~ ^[0-9]+$ ]]; then
    printf 'VM_RUN_STARTED_AT must be the run'"'"'s start in seconds since the epoch (got [%s]); without it tonight cannot be kept out of its own window\n' "$started" >&2
    return 2
  fi
  date -u -d "@$started" +%Y-%m-%d
}

vm_query_nightly() {
  local metric="${1:?Usage: vm_query_nightly <metric> <selector> <dates>}"
  local selector="${2:-}" dates="${3:?Usage: vm_query_nightly <metric> <selector> <dates>}"
  local ns="${VM_NAMESPACE:-monitoring}"
  local svc="${VM_SERVICE:-monitoring-victoriametrics}"
  local image="${VM_CURL_IMAGE:-docker.io/curlimages/curl:8.11.1}"
  local tonight start end response

  tonight="$(vm_tonight)" || return 2
  # The store's whole retention, to the end of tonight, which is what a re-run
  # of tonight wrote into. The dates are chosen from what comes back.
  start="$(date -u -d "$tonight - $VM_RETENTION_DAYS days" +%s)"
  end="$(date -u -d "$tonight + 1 day" +%s)"

  if ! response="$(kubectl -n "$ns" run "vm-query-$$" --rm -i --restart=Never \
    --image="$image" -- \
    curl -sS --max-time 30 -G "http://${svc}.${ns}.svc:8428/api/v1/export" \
    --data-urlencode "match[]=${metric}{${selector}}" \
    --data-urlencode "start=${start}" \
    --data-urlencode "end=${end}" </dev/null 2>/dev/null)"; then
    response=""
  fi

  jq -Rsr --arg tonight "$tonight" --argjson dates "$dates" '
    split("\n")
    | map(fromjson? // empty)
    | [ .[]
        | (.metric | del(.__name__) | to_entries | map("\(.key)=\(.value)") | sort | join(",")) as $sig
        | [.timestamps, .values] | transpose[]
        | { sig: $sig, ts: .[0], value: .[1],
            date: (.[0] / 1000 | floor | strftime("%Y-%m-%d")) }
        | select(.date < $tonight) ]
    | group_by([.sig, .date]) | map(max_by(.ts))
    | group_by(.sig) | map(sort_by(.date) | .[-$dates:])
    | .[][]
    | "\(.sig)\t\(.date)\t\(.value)"
  ' <<<"$response" 2>/dev/null || true
}

vm_nightly_window() {
  local nights
  nights="$(vm_query_nightly "$@")" || return 2
  jq -Rsr '
    split("\n")
    | map(select(length > 0) | split("\t") | {sig: .[0], date: .[1], value: (.[2] | tonumber)})
    | group_by(.sig)[]
    | (map(.value) | sort) as $v
    | ($v | length) as $n
    | (if $n % 2 == 1 then $v[($n - 1) / 2] else ($v[$n / 2 - 1] + $v[$n / 2]) / 2 end) as $median
    | "\(.[0].sig)\t\($median)\t\($n)\t\(max_by(.date).value)"
  ' <<<"$nights" 2>/dev/null || true
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  echo "vm-query.sh is a sourced library; source it and call vm_query_nightly or vm_nightly_window" >&2
  exit 2
fi
