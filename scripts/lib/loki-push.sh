#!/usr/bin/env bash
# Shared Loki push transport for the nightly trend pipelines
# (mutation / pmat / terraform-drift loki-push scripts). Reads the request body
# on stdin and POSTs it to Loki's /loki/api/v1/push.
#
# LOKI_PUSH_MODE selects the transport. The default reproduces the pre-cutover
# behavior exactly, so sourcing this changes nothing until the variable is set:
#
#   ssh-docker (default) — SSH to the VPS, run a throwaway curl container on the
#                          compose monitoring bridge network so it resolves
#                          `loki:3100`.
#   kubectl              — Cutover path (ADR-030): a throwaway curl pod in the
#                          cluster resolving the in-cluster Loki Service. The
#                          calling workflow must provide a kubeconfig
#                          (.github/actions/oci-kube-setup). Tunables:
#                          LOKI_NAMESPACE (default monitoring),
#                          LOKI_SERVICE (default monitoring-loki).

loki_push() {
  case "${LOKI_PUSH_MODE:-ssh-docker}" in
    ssh-docker)
      ssh -o StrictHostKeyChecking=accept-new deploy-target \
        'docker run --rm -i --network opengate-monitoring_monitoring \
          curlimages/curl:latest \
          -sS --fail --max-time 30 \
          -X POST http://loki:3100/loki/api/v1/push \
          -H "Content-Type: application/json" \
          --data-binary @-'
      ;;
    kubectl)
      local ns="${LOKI_NAMESPACE:-monitoring}"
      local svc="${LOKI_SERVICE:-monitoring-loki}"
      kubectl -n "$ns" run "loki-push-$$" --rm -i --restart=Never \
        --image=curlimages/curl:8.11.1 -- \
        curl -sS --fail --max-time 30 \
        -X POST "http://${svc}.${ns}.svc:3100/loki/api/v1/push" \
        -H "Content-Type: application/json" \
        --data-binary @-
      ;;
    *)
      echo "loki_push: unknown LOKI_PUSH_MODE='${LOKI_PUSH_MODE}' (want ssh-docker|kubectl)" >&2
      return 2
      ;;
  esac
}
