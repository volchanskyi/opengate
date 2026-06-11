#!/usr/bin/env bash
# pmat-loki-query.sh — read the most recent value of a pmat-trend field from
# Loki on the production VPS, so pmat-trend.yml can compute day-over-day
# regressions. Mirrors the SSH+docker transport of pmat-loki-push.sh but issues
# a read-only instant query.
#
# Usage: $0 <repo_score|below_bplus>
# Prints the scalar value, or nothing when absent.
#
# Fail-soft by design: a Loki blip (or the very first run, with no prior data)
# prints nothing, so the summarizer treats the previous value as null and does
# NOT raise a false day-over-day alert. Today's value is still pushed, so the
# next run has a baseline to compare against.
#
# The stream is single-series ({job,env} only — see pmat-loki-push.sh), so
# `last_over_time(... [14d])` yields exactly one result whose value is the most
# recent push within the window.
set -uo pipefail

FIELD="${1:?Usage: $0 <repo_score|below_bplus>}"
case "$FIELD" in
  repo_score|below_bplus) ;;
  *) echo "unknown field: $FIELD (want repo_score|below_bplus)" >&2; exit 2 ;;
esac

QUERY="last_over_time({job=\"pmat-trend\"} | json | unwrap ${FIELD} [14d])"

# LOKI_PUSH_MODE selects the transport, mirroring scripts/lib/loki-push.sh so the
# read and write halves stay symmetric. kubectl runs a throwaway curl pod against
# the in-cluster Loki Service; ssh-docker is the pre-cutover default.
case "${LOKI_PUSH_MODE:-ssh-docker}" in
  kubectl)
    ns="${LOKI_NAMESPACE:-monitoring}"
    svc="${LOKI_SERVICE:-monitoring-loki}"
    RESP="$(kubectl -n "$ns" run "loki-query-$$" --rm -i --restart=Never \
      --image=curlimages/curl:8.11.1 -- \
      curl -sS --max-time 30 -G "http://${svc}.${ns}.svc:3100/loki/api/v1/query" \
      --data-urlencode "query=${QUERY}" </dev/null 2>/dev/null || true)"
    ;;
  *)
    RESP="$(ssh -o StrictHostKeyChecking=accept-new deploy-target \
      "docker run --rm --network opengate-monitoring_monitoring curlimages/curl:latest \
         -sS --max-time 30 -G http://loki:3100/loki/api/v1/query \
         --data-urlencode 'query=${QUERY}'" 2>/dev/null || true)"
    ;;
esac

printf '%s' "$RESP" | jq -r '.data.result[0].value[1] // empty' 2>/dev/null || true
