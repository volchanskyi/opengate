#!/usr/bin/env bash
# Shared VictoriaMetrics read-back library for CI trend pipelines. Sourced like
# scripts/lib/vm-push.sh; provides two read-only modes against the in-cluster
# VictoriaMetrics Service via a throwaway kubectl curl pod:
#
#   vm_query_latest <metric> <selector>  — newest single sample via /api/v1/export
#                                           (max_by timestamp). Prints a scalar or
#                                           nothing. This is the PMAT day-over-day path.
#   vm_query_window <promql>             — window statistic via /api/v1/query. Prints
#                                           per-series values keyed by sorted labels
#                                           ("k=v,k=v<TAB>value"), one line per series,
#                                           so multi-dimensional gates can compare each
#                                           series independently.
#
# Both modes are FAIL-OPEN: any transport, empty, or parse failure yields empty
# output and exit 0 — a regression gate must never fail the build on infra flakiness.
#
# When VM_EXCLUDE_COMMIT is set, vm_query_latest appends commit!="$VM_EXCLUDE_COMMIT"
# to its selector so a workflow re-run never compares against its own just-pushed
# sample. Window consumers compose the same exclusion into their PromQL via
# vm_commit_exclusion (below), keeping the exclusion policy in one place.
#
# Transport tunables (shared with lib/vm-push.sh):
# VM_NAMESPACE (default monitoring), VM_SERVICE (default monitoring-victoriametrics),
# VM_CURL_IMAGE (default docker.io/curlimages/curl:8.11.1). The caller must provide
# a kubeconfig.

# Emit a label-matcher fragment excluding the current commit, or nothing when
# VM_EXCLUDE_COMMIT is unset. Consumers splice this into a selector or PromQL.
vm_commit_exclusion() {
  local sha="${VM_EXCLUDE_COMMIT:-}"
  if [ -n "$sha" ]; then
    printf 'commit!="%s"' "$sha"
  fi
}

# Combine a base label selector with the current-commit exclusion, joining with a
# comma only when both parts are non-empty. Prints the merged selector body.
vm_query_selector() {
  local selector="${1:-}"
  local exclusion
  exclusion="$(vm_commit_exclusion)"
  if [ -n "$selector" ] && [ -n "$exclusion" ]; then
    printf '%s,%s' "$selector" "$exclusion"
  else
    printf '%s%s' "$selector" "$exclusion"
  fi
}

# Newest single sample for <metric>{<selector>} over a 30d window. The export API
# returns one JSON object per series; the mandatory commit label creates one series
# per run, so flatten every point and select the newest timestamp rather than the
# highest value. Prints the scalar or nothing; never exits non-zero.
vm_query_latest() {
  local metric="${1:?Usage: vm_query_latest <metric> <selector>}"
  local selector="${2:-}"
  local ns="${VM_NAMESPACE:-monitoring}"
  local svc="${VM_SERVICE:-monitoring-victoriametrics}"
  local image="${VM_CURL_IMAGE:-docker.io/curlimages/curl:8.11.1}"
  local end start match response

  end="$(date -u +%s)"
  start="$((end - 30 * 24 * 60 * 60))"
  match="${metric}{$(vm_query_selector "$selector")}"

  if ! response="$(kubectl -n "$ns" run "vm-query-$$" --rm -i --restart=Never \
    --image="$image" -- \
    curl -sS --max-time 30 -G "http://${svc}.${ns}.svc:8428/api/v1/export" \
    --data-urlencode "match[]=${match}" \
    --data-urlencode "start=${start}" \
    --data-urlencode "end=${end}" </dev/null 2>/dev/null)"; then
    response=""
  fi

  printf '%s\n' "$response" | jq -Rsr '
    split("\n")
    | map(fromjson?)
    |
    [
      .[]
      | .values as $values
      | .timestamps as $timestamps
      | range(0; ($values | length)) as $index
      | {timestamp: $timestamps[$index], value: $values[$index]}
    ]
    | if length == 0 then empty else max_by(.timestamp).value end
  ' 2>/dev/null || true
}

# Window statistic for an arbitrary <promql>. Parses the /api/v1/query instant
# vector into one line per series: the series labels sorted and joined as
# "k=v,k=v", a tab, then the sample value. Prints nothing on any failure.
vm_query_window() {
  local promql="${1:?Usage: vm_query_window <promql>}"
  local ns="${VM_NAMESPACE:-monitoring}"
  local svc="${VM_SERVICE:-monitoring-victoriametrics}"
  local image="${VM_CURL_IMAGE:-docker.io/curlimages/curl:8.11.1}"
  local response

  if ! response="$(kubectl -n "$ns" run "vm-query-$$" --rm -i --restart=Never \
    --image="$image" -- \
    curl -sS --max-time 30 -G "http://${svc}.${ns}.svc:8428/api/v1/query" \
    --data-urlencode "query=${promql}" </dev/null 2>/dev/null)"; then
    response=""
  fi

  printf '%s\n' "$response" | jq -r '
    .data.result[]?
    | (.metric // {}) as $m
    | ([$m | to_entries[] | "\(.key)=\(.value)"] | sort | join(",")) as $sig
    | "\($sig)\t\(.value[1])"
  ' 2>/dev/null || true
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  echo "vm-query.sh is a sourced library; source it and call vm_query_latest or vm_query_window" >&2
  exit 2
fi
