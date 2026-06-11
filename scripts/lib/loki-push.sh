#!/usr/bin/env bash
# Shared Loki push transport for the nightly trend pipelines
# (mutation / pmat / terraform-drift loki-push scripts). Reads the request body
# on stdin and POSTs it from a throwaway curl pod to the in-cluster Loki
# Service. The calling workflow must provide a kubeconfig. Tunables:
# LOKI_NAMESPACE (default monitoring) and LOKI_SERVICE (default monitoring-loki).

loki_push() {
  local ns="${LOKI_NAMESPACE:-monitoring}"
  local svc="${LOKI_SERVICE:-monitoring-loki}"
  kubectl -n "$ns" run "loki-push-$$" --rm -i --restart=Never \
    --image=curlimages/curl:8.11.1 -- \
    curl -sS --fail --max-time 30 \
    -X POST "http://${svc}.${ns}.svc:3100/loki/api/v1/push" \
    -H "Content-Type: application/json" \
    --data-binary @-
}
