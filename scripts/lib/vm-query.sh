#!/usr/bin/env bash
# Shared VictoriaMetrics read-back library for CI trend pipelines. Sourced like
# scripts/lib/vm-push.sh; reads the in-cluster VictoriaMetrics Service through a
# throwaway kubectl curl pod.
#
#   vm_query_nightly <metric> <selector> <dates>
#       For each measurement, the latest reading of each of the <dates> most
#       recent dates before tonight's that have one, within the store's thirty
#       days, one line each: "k=v,k=v<TAB>YYYY-MM-DD<TAB>value", the labels
#       sorted. A weekly run's dates are a week apart and count the same.
#   vm_nightly_window <metric> <selector> <dates>
#       The same readings reduced to one line per measurement:
#       "k=v,k=v<TAB>median<TAB>count<TAB>newest" — the median over its dates,
#       how many dates, and the newest date's reading.
#
# A night is a date. A sample carries the time its run started, so however long
# a run takes it lands on one date, and a re-run lands on the same one. Tonight
# is the date of VM_RUN_STARTED_AT, and it is left out whatever code wrote it:
# nights on the same commit count, and tonight never judges itself.
#
# Transport, empty and parse failures are FAIL-OPEN: they print nothing and exit
# 0, because a regression gate must never fail the build on infrastructure. A
# reader that does not know tonight's date refuses: that is a setup defect, and
# it would read tonight into its own window.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
#   VM_NAMESPACE (default monitoring), VM_SERVICE (default
#   monitoring-victoriametrics), VM_CURL_IMAGE (default
#   docker.io/curlimages/curl:8.11.1). The caller must provide a kubeconfig.

# How far back the store keeps anything (the chart's -retentionPeriod).
VM_RETENTION_DAYS=30

# vm_tonight prints tonight's date, or refuses.
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
