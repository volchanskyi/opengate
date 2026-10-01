#!/usr/bin/env bash
# Shared VictoriaMetrics push transport for CI trend pipelines. Reads
# Prometheus text from a file argument or stdin, holds every sample to the shape
# the trend store keeps, and POSTs it from a throwaway curl pod to the
# in-cluster VictoriaMetrics Service. The calling workflow must provide a
# kubeconfig.
#
# A sample's labels name the measurement and nothing else. A label that changes
# every run — the commit, the run, a grade — makes every run a series of its own,
# and a trend drawn from those is one colour a night joined by nothing. So such
# a sample is refused, every sample is stamped with the time its run started
# (so a night is one point on one date, however long the run took, and a re-run
# writes the same point), and each push writes one
# ci_run_info{workflow,commit,run_id} beside it, so a night still names its
# code.
#
# Environment:
#   VM_RUN_STARTED_AT  the run's start, in seconds since the epoch (required)
#   VM_NAMESPACE (default monitoring), VM_SERVICE (default
#   monitoring-victoriametrics), VM_CURL_IMAGE (default
#   docker.io/curlimages/curl:8.11.1).

# The labels a sample may not carry: each changes every run.
VM_PER_RUN_LABELS='commit|run_id|grade'

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
    if [[ "$line" =~ [\{,]($VM_PER_RUN_LABELS)= ]]; then
      printf 'a sample names the measurement only; %s changes every run, at %s:%s\n' \
        "${BASH_REMATCH[1]}" "$file" "$line_no" >&2
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

# vm_label_escape escapes a label value for Prometheus text.
vm_label_escape() {
  sed 's/\\/\\\\/g; s/"/\\"/g' <<<"$1"
}

# vm_stamp FILE STARTED prints the samples stamped with the run's start, in
# milliseconds, and the one ci_run_info the push carries beside them.
vm_stamp() {
  local file="$1" started="$2" ms
  ms="${started}000"
  awk -v ms="$ms" '/^[[:space:]]*(#|$)/ { next } { print $0 " " ms }' "$file"
  printf 'ci_run_info{env="ci",workflow="%s",commit="%s",run_id="%s"} 1 %s\n' \
    "$(vm_label_escape "${GITHUB_WORKFLOW:-local}")" \
    "$(vm_label_escape "${GITHUB_SHA:-unknown}")" \
    "$(vm_label_escape "${GITHUB_RUN_ID:-local}")" "$ms"
}

vm_push() {
  local ns="${VM_NAMESPACE:-monitoring}"
  local svc="${VM_SERVICE:-monitoring-victoriametrics}"
  local image="${VM_CURL_IMAGE:-docker.io/curlimages/curl:8.11.1}"
  local started="${VM_RUN_STARTED_AT:-}"
  local payload_file="${1:-}"
  local tmp_file="" stamped

  if [[ ! "$started" =~ ^[0-9]+$ ]]; then
    printf 'VM_RUN_STARTED_AT must be the run'"'"'s start in seconds since the epoch (got [%s]); every sample of the night carries it\n' "$started" >&2
    return 1
  fi

  if [ -z "$payload_file" ]; then
    tmp_file="$(mktemp)"
    cat >"$tmp_file"
    payload_file="$tmp_file"
  elif [ ! -f "$payload_file" ]; then
    printf 'Prometheus payload file not found: %s\n' "$payload_file" >&2
    return 1
  fi

  if ! vm_validate_prometheus_text "$payload_file"; then
    [ -z "$tmp_file" ] || rm -f "$tmp_file"
    return 1
  fi

  stamped="$(mktemp)"
  vm_stamp "$payload_file" "$started" >"$stamped"
  [ -z "$tmp_file" ] || rm -f "$tmp_file"

  local status=0
  kubectl -n "$ns" run "vm-push-$$" --rm -i --restart=Never \
    --image="$image" -- \
    curl -sS --fail --max-time 30 \
    -X POST "http://${svc}.${ns}.svc:8428/api/v1/import/prometheus" \
    -H "Content-Type: text/plain; version=0.0.4" \
    --data-binary @- <"$stamped" || status=$?

  rm -f "$stamped"
  return "$status"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  set -euo pipefail
  vm_push "${1:-}"
fi
