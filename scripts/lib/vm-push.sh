#!/usr/bin/env bash
# Shared VictoriaMetrics push transport for CI trend pipelines. Reads
# Prometheus text from a file argument or stdin, validates the project-wide
# mandatory CI labels, and POSTs it from a throwaway curl pod to the in-cluster
# VictoriaMetrics Service. The calling workflow must provide a kubeconfig.
# Tunables:
# VM_NAMESPACE (default monitoring), VM_SERVICE (default monitoring-victoriametrics),
# and VM_CURL_IMAGE (default docker.io/curlimages/curl:8.11.1).

vm_validate_prometheus_text() {
  local file="$1"
  local line line_no=0 samples=0

  while IFS= read -r line || [ -n "$line" ]; do
    line_no=$((line_no + 1))
    case "$line" in
      "" | "#"*) continue ;;
    esac

    if [[ ! "$line" =~ ^[a-zA-Z_:][a-zA-Z0-9_:]*\{[^}]*\}[[:space:]]+[-+]?([0-9]+([.][0-9]+)?|[.][0-9]+)([eE][-+]?[0-9]+)?$ ]]; then
      printf 'invalid Prometheus sample at %s:%s\n' "$file" "$line_no" >&2
      return 1
    fi
    if [[ ! "$line" =~ \{[^}]*commit=\"[^\"]+\"[^}]*\} ]]; then
      printf 'missing mandatory commit label at %s:%s\n' "$file" "$line_no" >&2
      return 1
    fi
    if [[ ! "$line" =~ \{[^}]*env=\"ci\"[^}]*\} ]]; then
      printf 'missing mandatory env="ci" label at %s:%s\n' "$file" "$line_no" >&2
      return 1
    fi
    samples=$((samples + 1))
  done <"$file"

  if [ "$samples" -eq 0 ]; then
    printf 'no Prometheus samples found in %s\n' "$file" >&2
    return 1
  fi
}

vm_push() {
  local ns="${VM_NAMESPACE:-monitoring}"
  local svc="${VM_SERVICE:-monitoring-victoriametrics}"
  local image="${VM_CURL_IMAGE:-docker.io/curlimages/curl:8.11.1}"
  local payload_file="${1:-}"
  local tmp_file=""

  if [ -z "$payload_file" ]; then
    tmp_file="$(mktemp)"
    cat >"$tmp_file"
    payload_file="$tmp_file"
  elif [ ! -f "$payload_file" ]; then
    printf 'Prometheus payload file not found: %s\n' "$payload_file" >&2
    return 1
  fi

  if ! vm_validate_prometheus_text "$payload_file"; then
    if [ -n "$tmp_file" ]; then
      rm -f "$tmp_file"
    fi
    return 1
  fi

  local status=0
  kubectl -n "$ns" run "vm-push-$$" --rm -i --restart=Never \
    --image="$image" -- \
    curl -sS --fail --max-time 30 \
    -X POST "http://${svc}.${ns}.svc:8428/api/v1/import/prometheus" \
    -H "Content-Type: text/plain; version=0.0.4" \
    --data-binary @- <"$payload_file" || status=$?

  if [ -n "$tmp_file" ]; then
    rm -f "$tmp_file"
  fi
  return "$status"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  vm_push "${1:-}"
fi
