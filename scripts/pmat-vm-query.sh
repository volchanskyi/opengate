#!/usr/bin/env bash
# Read the most recent PMAT value from VictoriaMetrics so the nightly workflow
# can compute day-over-day regressions before publishing the current sample.
#
# Usage: $0 <repo_score|below_bplus>
# Prints the scalar value, or nothing when history is absent/unavailable.

set -uo pipefail

FIELD="${1:?Usage: $0 <repo_score|below_bplus>}"
case "$FIELD" in
  repo_score) metric="pmat_repo_score" ;;
  below_bplus) metric="pmat_below_bplus" ;;
  *)
    echo "unknown field: $FIELD (want repo_score|below_bplus)" >&2
    exit 2
    ;;
esac

ns="${VM_NAMESPACE:-monitoring}"
svc="${VM_SERVICE:-monitoring-victoriametrics}"
image="${VM_CURL_IMAGE:-docker.io/curlimages/curl:8.11.1}"
end="$(date -u +%s)"
start="$((end - 30 * 24 * 60 * 60))"

if ! response="$(kubectl -n "$ns" run "vm-query-$$" --rm -i --restart=Never \
  --image="$image" -- \
  curl -sS --max-time 30 -G "http://${svc}.${ns}.svc:8428/api/v1/export" \
  --data-urlencode "match[]=${metric}{env=\"ci\"}" \
  --data-urlencode "start=${start}" \
  --data-urlencode "end=${end}" </dev/null 2>/dev/null)"; then
  response=""
fi

# The export API returns one JSON object per series. The mandatory commit label
# creates one series per run, so flatten every point and select the newest
# timestamp rather than accidentally selecting the highest metric value.
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
